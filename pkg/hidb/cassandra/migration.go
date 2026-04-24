package cassandra

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Migration describes a single versioned schema change.
type Migration struct {
	Version int64  // strictly increasing; often a UNIX timestamp
	Name    string // human readable
	Up      func(ctx context.Context, db *CassandraDB) error
	Down    func(ctx context.Context, db *CassandraDB) error
}

// Migrator tracks and applies migrations. It stores applied versions in a
// tracking table inside the session keyspace.
type Migrator struct {
	db             *CassandraDB
	trackingTable  string
	migrations     []Migration
	trackKeyspace  string
	mu             sync.Mutex
}

// NewMigrator creates a migrator. The tracking table is created on first use.
// If keyspace is empty the session keyspace is used.
func NewMigrator(db *CassandraDB, keyspace, trackingTable string) *Migrator {
	if trackingTable == "" {
		trackingTable = "schema_migrations"
	}
	return &Migrator{db: db, trackKeyspace: keyspace, trackingTable: trackingTable}
}

// Register adds migrations to the migrator. Safe to call multiple times;
// migrations are sorted by Version before execution.
func (m *Migrator) Register(migrations ...Migration) *Migrator {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.migrations = append(m.migrations, migrations...)
	return m
}

func (m *Migrator) trackingRef() string {
	if m.trackKeyspace != "" {
		return quoteIdent(m.trackKeyspace) + "." + quoteIdent(m.trackingTable)
	}
	return quoteIdent(m.trackingTable)
}

// Init creates the tracking table if it does not exist.
func (m *Migrator) Init(ctx context.Context) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  version bigint,
  name text,
  applied_at timestamp,
  PRIMARY KEY (version)
)`, m.trackingRef())
	return m.db.Exec(ctx, stmt)
}

// applied returns the set of already-applied versions.
func (m *Migrator) applied(ctx context.Context) (map[int64]bool, error) {
	stmt := fmt.Sprintf("SELECT version FROM %s", m.trackingRef())
	iter := m.db.session.Query(stmt).WithContext(ctx).Iter()
	defer iter.Close()
	var v int64
	out := make(map[int64]bool)
	for iter.Scan(&v) {
		out[v] = true
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// Up applies all pending migrations in version order.
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.Init(ctx); err != nil {
		return err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	pending := make([]Migration, 0, len(m.migrations))
	for _, mig := range m.migrations {
		if !applied[mig.Version] {
			pending = append(pending, mig)
		}
	}
	m.mu.Unlock()
	sort.Slice(pending, func(i, j int) bool { return pending[i].Version < pending[j].Version })

	for _, mig := range pending {
		if mig.Up == nil {
			return fmt.Errorf("cassandra: migration %d (%s) has no Up", mig.Version, mig.Name)
		}
		if err := mig.Up(ctx, m.db); err != nil {
			return fmt.Errorf("cassandra: migration %d (%s) failed: %w", mig.Version, mig.Name, err)
		}
		stmt := fmt.Sprintf("INSERT INTO %s (version, name, applied_at) VALUES (?, ?, ?)", m.trackingRef())
		if err := m.db.session.Query(stmt, mig.Version, mig.Name, time.Now()).WithContext(ctx).Exec(); err != nil {
			return err
		}
	}
	return nil
}

// Down rolls back the most recent migration.
func (m *Migrator) Down(ctx context.Context) error {
	applied, err := m.applied(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return nil
	}
	versions := make([]int64, 0, len(applied))
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] > versions[j] })
	latest := versions[0]

	m.mu.Lock()
	var target Migration
	found := false
	for _, mig := range m.migrations {
		if mig.Version == latest {
			target = mig
			found = true
			break
		}
	}
	m.mu.Unlock()

	if !found {
		return fmt.Errorf("cassandra: no registered migration for version %d", latest)
	}
	if target.Down == nil {
		return fmt.Errorf("cassandra: migration %d (%s) has no Down", target.Version, target.Name)
	}
	if err := target.Down(ctx, m.db); err != nil {
		return err
	}
	stmt := fmt.Sprintf("DELETE FROM %s WHERE version = ?", m.trackingRef())
	return m.db.session.Query(stmt, latest).WithContext(ctx).Exec()
}

// Status returns one line per migration (applied or pending).
func (m *Migrator) Status(ctx context.Context) (string, error) {
	applied, err := m.applied(ctx)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sorted := make([]Migration, len(m.migrations))
	copy(sorted, m.migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version < sorted[j].Version })
	var sb strings.Builder
	for _, mig := range sorted {
		mark := "pending"
		if applied[mig.Version] {
			mark = "applied"
		}
		fmt.Fprintf(&sb, "[%s] %d %s\n", mark, mig.Version, mig.Name)
	}
	return sb.String(), nil
}
