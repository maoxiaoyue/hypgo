package cassandra

import (
	"context"
	"fmt"
	"net"
	"time"
)

// NodetoolInfo aggregates node-level facts from system.local.
// It maps to the most common fields printed by `nodetool info` and `nodetool status`.
type NodetoolInfo struct {
	HostID         string
	ClusterName    string
	DataCenter     string
	Rack           string
	ReleaseVersion string
	CQLVersion     string
	Partitioner    string
	BroadcastAddr  net.IP
	ListenAddr     net.IP
	RPCAddr        net.IP
	SchemaVersion  string
	Tokens         []string
}

// PeerInfo is one entry from system.peers_v2.
type PeerInfo struct {
	PeerAddr       net.IP
	PeerPort       int
	DataCenter     string
	Rack           string
	HostID         string
	ReleaseVersion string
	SchemaVersion  string
	Tokens         []string
}

// ClusterSnapshot combines local + peers to approximate `nodetool status`.
type ClusterSnapshot struct {
	Local NodetoolInfo
	Peers []PeerInfo
}

// LocalInfo reads system.local (equivalent of `nodetool info` subset).
func (c *CassandraDB) LocalInfo(ctx context.Context) (*NodetoolInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	var info NodetoolInfo
	err := c.session.Query(
		`SELECT host_id, cluster_name, data_center, rack, release_version,
		        cql_version, partitioner, broadcast_address, listen_address,
		        rpc_address, schema_version, tokens
		 FROM system.local WHERE key = 'local'`,
	).WithContext(ctx).Scan(
		&info.HostID, &info.ClusterName, &info.DataCenter, &info.Rack, &info.ReleaseVersion,
		&info.CQLVersion, &info.Partitioner, &info.BroadcastAddr, &info.ListenAddr,
		&info.RPCAddr, &info.SchemaVersion, &info.Tokens,
	)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Peers reads system.peers_v2 (equivalent of `nodetool status` minus load metrics).
func (c *CassandraDB) Peers(ctx context.Context) ([]PeerInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT peer, peer_port, data_center, rack, host_id, release_version, schema_version, tokens
		 FROM system.peers_v2`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []PeerInfo
	var p PeerInfo
	for iter.Scan(&p.PeerAddr, &p.PeerPort, &p.DataCenter, &p.Rack,
		&p.HostID, &p.ReleaseVersion, &p.SchemaVersion, &p.Tokens) {
		out = append(out, p)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// ClusterStatus returns a full cluster snapshot (local + peers).
func (c *CassandraDB) ClusterStatus(ctx context.Context) (*ClusterSnapshot, error) {
	local, err := c.LocalInfo(ctx)
	if err != nil {
		return nil, err
	}
	peers, err := c.Peers(ctx)
	if err != nil {
		return nil, err
	}
	return &ClusterSnapshot{Local: *local, Peers: peers}, nil
}

// ===== system_views (Cassandra 4.0+/5.0 virtual keyspace) =====

// ThreadPoolStat represents one row of system_views.thread_pools (tpstats).
type ThreadPoolStat struct {
	Name            string
	ActiveTasks     int
	PendingTasks    int
	CompletedTasks  int64
	BlockedTasks    int
	TotalBlocked    int64
	MaxPoolSize     int
	CoreActive      int
}

// ThreadPools is equivalent of `nodetool tpstats`.
func (c *CassandraDB) ThreadPools(ctx context.Context) ([]ThreadPoolStat, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT name, active_tasks, pending_tasks, completed_tasks,
		        blocked_tasks, total_blocked_tasks, max_pool_size, active_tasks_limit
		 FROM system_views.thread_pools`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []ThreadPoolStat
	var s ThreadPoolStat
	for iter.Scan(&s.Name, &s.ActiveTasks, &s.PendingTasks, &s.CompletedTasks,
		&s.BlockedTasks, &s.TotalBlocked, &s.MaxPoolSize, &s.CoreActive) {
		out = append(out, s)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// SSTableTask is one row of system_views.sstable_tasks (compactionstats).
type SSTableTask struct {
	Keyspace   string
	Table      string
	TaskID     string
	Kind       string // compaction, cleanup, scrub, upgrade_sstables, ...
	Progress   int64
	Total      int64
	Unit       string
}

// CompactionTasks is equivalent of `nodetool compactionstats`.
func (c *CassandraDB) CompactionTasks(ctx context.Context) ([]SSTableTask, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, task_id, kind, progress, total, unit
		 FROM system_views.sstable_tasks`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []SSTableTask
	var t SSTableTask
	for iter.Scan(&t.Keyspace, &t.Table, &t.TaskID, &t.Kind, &t.Progress, &t.Total, &t.Unit) {
		out = append(out, t)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// ClientConnection is one row of system_views.clients (clientstats).
type ClientConnection struct {
	Address       net.IP
	Port          int
	Hostname      string
	Username      string
	ConnectionStage string
	Driver        string
	DriverVersion string
	Protocol      int
	SSLEnabled    bool
	SSLProtocol   string
	SSLCipher     string
	RequestCount  int64
}

// Clients is equivalent of `nodetool clientstats --all`.
func (c *CassandraDB) Clients(ctx context.Context) ([]ClientConnection, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT address, port, hostname, username, connection_stage, driver_name,
		        driver_version, protocol_version, ssl_enabled, ssl_protocol,
		        ssl_cipher_suite, request_count
		 FROM system_views.clients`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []ClientConnection
	var cc ClientConnection
	for iter.Scan(&cc.Address, &cc.Port, &cc.Hostname, &cc.Username, &cc.ConnectionStage,
		&cc.Driver, &cc.DriverVersion, &cc.Protocol, &cc.SSLEnabled, &cc.SSLProtocol,
		&cc.SSLCipher, &cc.RequestCount) {
		out = append(out, cc)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// Setting is one row of system_views.settings (getlogginglevels, etc.).
type Setting struct {
	Name  string
	Value string
}

// Settings is equivalent of `nodetool getlogginglevels` + many get* commands.
func (c *CassandraDB) Settings(ctx context.Context) ([]Setting, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT name, value FROM system_views.settings`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []Setting
	var s Setting
	for iter.Scan(&s.Name, &s.Value) {
		out = append(out, s)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// Setting returns the current value of a single setting by name (e.g. "compaction_throughput_mb_per_sec").
func (c *CassandraDB) Setting(ctx context.Context, name string) (string, error) {
	if c.session == nil {
		return "", fmt.Errorf("cassandra: session not connected")
	}
	var v string
	err := c.session.Query(
		`SELECT value FROM system_views.settings WHERE name = ?`, name,
	).WithContext(ctx).Scan(&v)
	return v, err
}

// CacheStat is one row of system_views.caches (info cache ratios).
type CacheStat struct {
	Name       string
	CapacityB  int64
	Entries    int
	Size       int64
	HitRate    float64
	Hits       int64
	Requests   int64
	Recent     float64
}

// Caches is equivalent of `nodetool info` cache lines.
func (c *CassandraDB) Caches(ctx context.Context) ([]CacheStat, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT name, capacity_bytes, entry_count, size_bytes, hit_ratio,
		        hit_count, request_count, recent_hit_rate_per_second
		 FROM system_views.caches`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []CacheStat
	var s CacheStat
	for iter.Scan(&s.Name, &s.CapacityB, &s.Entries, &s.Size, &s.HitRate,
		&s.Hits, &s.Requests, &s.Recent) {
		out = append(out, s)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// SizeEstimate is one row of system.size_estimates (per-range size).
type SizeEstimate struct {
	Keyspace         string
	Table            string
	RangeStart       string
	RangeEnd         string
	MeanPartitionSize int64
	PartitionsCount   int64
}

// TableSizeEstimates returns size estimates for a table (per-range).
// Pass empty keyspace to use the session keyspace.
func (c *CassandraDB) TableSizeEstimates(ctx context.Context, keyspace, table string) ([]SizeEstimate, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	ks := keyspace
	if ks == "" {
		ks = c.config.Keyspace
	}
	if ks == "" || table == "" {
		return nil, fmt.Errorf("cassandra: keyspace and table are required")
	}
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, range_start, range_end,
		        mean_partition_size, partitions_count
		 FROM system.size_estimates WHERE keyspace_name = ? AND table_name = ?`,
		ks, table,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []SizeEstimate
	var s SizeEstimate
	for iter.Scan(&s.Keyspace, &s.Table, &s.RangeStart, &s.RangeEnd,
		&s.MeanPartitionSize, &s.PartitionsCount) {
		out = append(out, s)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// TableTotals aggregates SizeEstimates rows into a single total (approx).
type TableTotals struct {
	Keyspace        string
	Table           string
	EstimatedRows   int64
	EstimatedBytes  int64
}

// TableStats is equivalent of `nodetool tablestats ks.table` (size-focused subset).
func (c *CassandraDB) TableStats(ctx context.Context, keyspace, table string) (*TableTotals, error) {
	rows, err := c.TableSizeEstimates(ctx, keyspace, table)
	if err != nil {
		return nil, err
	}
	tot := &TableTotals{Keyspace: keyspace, Table: table}
	if keyspace == "" {
		tot.Keyspace = c.config.Keyspace
	}
	for _, r := range rows {
		tot.EstimatedRows += r.PartitionsCount
		tot.EstimatedBytes += r.PartitionsCount * r.MeanPartitionSize
	}
	return tot, nil
}

// ===== Virtual table helpers =====

// RawSystemViewRow returns all columns of a system_views table as a map.
// Useful for tables whose schema varies across versions.
func (c *CassandraDB) RawSystemViewRow(ctx context.Context, table string, where map[string]interface{}) ([]map[string]interface{}, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	stmt := "SELECT * FROM system_views." + quoteIdent(table)
	args := make([]interface{}, 0, len(where))
	if len(where) > 0 {
		stmt += " WHERE "
		first := true
		for k, v := range where {
			if !first {
				stmt += " AND "
			}
			first = false
			stmt += quoteIdent(k) + " = ?"
			args = append(args, v)
		}
	}
	iter := c.session.Query(stmt, args...).WithContext(ctx).Iter()
	defer iter.Close()

	var out []map[string]interface{}
	for {
		row := make(map[string]interface{})
		if !iter.MapScan(row) {
			break
		}
		out = append(out, row)
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// PollCompactionsUntilIdle polls sstable_tasks until empty or ctx expires.
// Returns the total wall time waited. Intended for callers that want to block
// until a triggered compaction finishes (e.g. after changing compaction config).
func (c *CassandraDB) PollCompactionsUntilIdle(ctx context.Context, interval time.Duration) (time.Duration, error) {
	if interval <= 0 {
		interval = time.Second
	}
	start := time.Now()
	for {
		tasks, err := c.CompactionTasks(ctx)
		if err != nil {
			return time.Since(start), err
		}
		if len(tasks) == 0 {
			return time.Since(start), nil
		}
		select {
		case <-ctx.Done():
			return time.Since(start), ctx.Err()
		case <-time.After(interval):
		}
	}
}
