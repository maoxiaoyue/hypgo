package cassandra

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// Config Cassandra 配置
type Config struct {
	Hosts            []string      `mapstructure:"hosts" yaml:"hosts"`
	Keyspace         string        `mapstructure:"keyspace" yaml:"keyspace"`
	Consistency      string        `mapstructure:"consistency" yaml:"consistency"`           // "one", "quorum", "all", "local_quorum" 等
	Port             int           `mapstructure:"port" yaml:"port"`                         // 預設 9042
	Username         string        `mapstructure:"username" yaml:"username"`
	Password         string        `mapstructure:"password" yaml:"password"`
	ConnectTimeout   time.Duration `mapstructure:"connect_timeout" yaml:"connect_timeout"`   // 預設 5s
	Timeout          time.Duration `mapstructure:"timeout" yaml:"timeout"`                   // 預設 10s
	NumConns         int           `mapstructure:"num_conns" yaml:"num_conns"`               // 每個主機的連接數
	MaxPreparedStmts int           `mapstructure:"max_prepared_stmts" yaml:"max_prepared_stmts"`
	ProtoVersion     int           `mapstructure:"proto_version" yaml:"proto_version"`       // 預設 4
}

// CassandraDB Cassandra 數據庫管理器
type CassandraDB struct {
	cluster *gocql.ClusterConfig
	session *gocql.Session
	config  Config
}

// New 創建 Cassandra 實例並建立連接
func New(cfg Config) (*CassandraDB, error) {
	c := &CassandraDB{
		config: cfg,
	}

	if err := c.init(); err != nil {
		return nil, err
	}

	if err := c.Connect(); err != nil {
		return nil, err
	}

	return c, nil
}

// NewWithoutConnect 創建 Cassandra 實例但不立即連接
// 適用於需要延遲連接的場景
func NewWithoutConnect(cfg Config) (*CassandraDB, error) {
	c := &CassandraDB{
		config: cfg,
	}

	if err := c.init(); err != nil {
		return nil, err
	}

	return c, nil
}

// init 初始化 cluster 配置
func (c *CassandraDB) init() error {
	if len(c.config.Hosts) == 0 {
		return fmt.Errorf("cassandra: at least one host is required")
	}

	cluster := gocql.NewCluster(c.config.Hosts...)

	// Keyspace
	if c.config.Keyspace != "" {
		cluster.Keyspace = c.config.Keyspace
	}

	// Consistency
	cluster.Consistency = parseConsistency(c.config.Consistency)

	// Port
	if c.config.Port > 0 {
		cluster.Port = c.config.Port
	}

	// 認證
	if c.config.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: c.config.Username,
			Password: c.config.Password,
		}
	}

	// 超時設置
	if c.config.ConnectTimeout > 0 {
		cluster.ConnectTimeout = c.config.ConnectTimeout
	} else {
		cluster.ConnectTimeout = 5 * time.Second
	}

	if c.config.Timeout > 0 {
		cluster.Timeout = c.config.Timeout
	} else {
		cluster.Timeout = 10 * time.Second
	}

	// 連接池
	if c.config.NumConns > 0 {
		cluster.NumConns = c.config.NumConns
	}

	if c.config.MaxPreparedStmts > 0 {
		cluster.MaxPreparedStmts = c.config.MaxPreparedStmts
	}

	// 協議版本
	if c.config.ProtoVersion > 0 {
		cluster.ProtoVersion = c.config.ProtoVersion
	} else {
		cluster.ProtoVersion = 4
	}

	c.cluster = cluster
	return nil
}

// Connect 建立 Cassandra 連接
func (c *CassandraDB) Connect() error {
	if c.cluster == nil {
		return fmt.Errorf("cassandra: cluster config not initialized")
	}

	session, err := c.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("cassandra: failed to create session: %w", err)
	}

	c.session = session
	return nil
}

// Session 獲取 gocql.Session（用戶直接操作 CQL）
func (c *CassandraDB) Session() *gocql.Session {
	return c.session
}

// Close 關閉 Cassandra 連接
func (c *CassandraDB) Close() error {
	if c.session != nil {
		c.session.Close()
		c.session = nil
	}
	return nil
}

// Ping 健康檢查
func (c *CassandraDB) Ping(ctx context.Context) error {
	if c.session == nil {
		return fmt.Errorf("cassandra: session not connected")
	}

	// 使用簡單查詢測試連接
	iter := c.session.Query("SELECT now() FROM system.local").WithContext(ctx).Iter()
	if err := iter.Close(); err != nil {
		return fmt.Errorf("cassandra: ping failed: %w", err)
	}

	return nil
}

// IsConnected 檢查是否已連接
func (c *CassandraDB) IsConnected() bool {
	if c.session == nil {
		return false
	}
	return !c.session.Closed()
}

// ===== DatabasePlugin 介面實現 =====
// 讓 CassandraDB 可以透過 hidb.Database.RegisterPlugin() 整合

// Name 返回插件名稱
func (c *CassandraDB) Name() string {
	return "cassandra"
}

// Init 從 map 配置初始化（用於插件系統動態加載）
func (c *CassandraDB) Init(config map[string]interface{}) error {
	// 從 map 解析配置
	if hosts, ok := config["hosts"].([]string); ok {
		c.config.Hosts = hosts
	}
	if keyspace, ok := config["keyspace"].(string); ok {
		c.config.Keyspace = keyspace
	}
	if consistency, ok := config["consistency"].(string); ok {
		c.config.Consistency = consistency
	}
	if port, ok := config["port"].(int); ok {
		c.config.Port = port
	}
	if username, ok := config["username"].(string); ok {
		c.config.Username = username
	}
	if password, ok := config["password"].(string); ok {
		c.config.Password = password
	}

	return c.init()
}

// ===== Cassandra 特殊功能 =====

// Query 執行 CQL 查詢（便捷方法）
func (c *CassandraDB) Query(stmt string, values ...interface{}) *gocql.Query {
	return c.session.Query(stmt, values...)
}

// QueryContext 執行帶 context 的 CQL 查詢
func (c *CassandraDB) QueryContext(ctx context.Context, stmt string, values ...interface{}) *gocql.Query {
	return c.session.Query(stmt, values...).WithContext(ctx)
}

// Exec executes a single CQL statement (DDL or DML) without returning rows.
// The statement may be terminated with a trailing ';', which is stripped
// before dispatch. For multi-statement scripts or parameterised CQL, use
// ExecScript or QueryContext respectively.
func (c *CassandraDB) Exec(ctx context.Context, stmt string) error {
	if c.session == nil {
		return fmt.Errorf("cassandra: session not connected")
	}
	stmt = strings.TrimSpace(stmt)
	stmt = strings.TrimRight(stmt, ";")
	stmt = strings.TrimRight(stmt, " \t\r\n")
	if stmt == "" {
		return nil
	}
	return c.session.Query(stmt).WithContext(ctx).Exec()
}

// ExecScript executes a multi-statement CQL script, ignoring empty segments.
// Bind args are not supported; use separate Exec calls for parameterised CQL.
func (c *CassandraDB) ExecScript(ctx context.Context, script string) error {
	for i, p := range splitStatements(script) {
		if err := c.session.Query(p).WithContext(ctx).Exec(); err != nil {
			return fmt.Errorf("cassandra: stmt %d failed: %w", i+1, err)
		}
	}
	return nil
}

// splitStatements splits a CQL script on top-level semicolons (ignoring those
// inside single-quoted literals and $$-delimited function bodies).
func splitStatements(src string) []string {
	var out []string
	var cur strings.Builder
	inSingle := false
	inDollar := false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		switch {
		case !inSingle && !inDollar && ch == '\'':
			inSingle = true
			cur.WriteByte(ch)
		case inSingle && ch == '\'':
			inSingle = false
			cur.WriteByte(ch)
		case !inSingle && ch == '$' && i+1 < len(src) && src[i+1] == '$':
			inDollar = !inDollar
			cur.WriteByte(ch)
			cur.WriteByte(src[i+1])
			i++
		case !inSingle && !inDollar && ch == ';':
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

// Batch 創建批次操作（gocql 原生介面）
func (c *CassandraDB) Batch(batchType gocql.BatchType) *gocql.Batch {
	return c.session.NewBatch(batchType)
}

// ExecuteBatch 執行批次操作（gocql 原生介面）
func (c *CassandraDB) ExecuteBatch(batch *gocql.Batch) error {
	return c.session.ExecuteBatch(batch)
}

// KeyspaceName returns the current session keyspace (from config).
func (c *CassandraDB) KeyspaceName() string {
	return c.config.Keyspace
}

// parseConsistency 將字串轉換為 gocql.Consistency
func parseConsistency(s string) gocql.Consistency {
	switch strings.ToLower(s) {
	case "any":
		return gocql.Any
	case "one":
		return gocql.One
	case "two":
		return gocql.Two
	case "three":
		return gocql.Three
	case "quorum":
		return gocql.Quorum
	case "all":
		return gocql.All
	case "local_quorum", "localquorum":
		return gocql.LocalQuorum
	case "each_quorum", "eachquorum":
		return gocql.EachQuorum
	case "local_one", "localone":
		return gocql.LocalOne
	default:
		return gocql.Quorum // 預設使用 Quorum
	}
}
