package cassandra

import (
	"context"
	"fmt"
	"strings"
)

// MaterializedViewBuilder builds CREATE / DROP MATERIALIZED VIEW statements.
type MaterializedViewBuilder struct {
	db           *CassandraDB
	keyspace     string
	name         string
	baseKeyspace string
	baseTable    string
	columns      []string // "*" for all
	where        []string
	pk           PrimaryKey
	orderBy      []Column
	options      TableOptions
	ifNotExist   bool
}

// MaterializedView returns a new MV builder.
func (c *CassandraDB) MaterializedView(name string) *MaterializedViewBuilder {
	ks, n := splitQualified(name)
	return &MaterializedViewBuilder{db: c, keyspace: ks, name: n, ifNotExist: true}
}

// IfNotExists toggles IF NOT EXISTS (default true).
func (m *MaterializedViewBuilder) IfNotExists(v bool) *MaterializedViewBuilder {
	m.ifNotExist = v
	return m
}

// FromTable sets the base table. Pass "ks.table" to include the keyspace.
func (m *MaterializedViewBuilder) FromTable(table string) *MaterializedViewBuilder {
	ks, t := splitQualified(table)
	m.baseKeyspace = ks
	m.baseTable = t
	return m
}

// Select chooses the projected columns. Use "*" to project all.
func (m *MaterializedViewBuilder) Select(columns ...string) *MaterializedViewBuilder {
	m.columns = append(m.columns, columns...)
	return m
}

// Where adds a condition (typically `col IS NOT NULL`).
// Cassandra requires all PK columns of the MV to appear in WHERE as IS NOT NULL.
func (m *MaterializedViewBuilder) Where(cond string) *MaterializedViewBuilder {
	m.where = append(m.where, cond)
	return m
}

// WhereNotNull adds `IS NOT NULL` clauses for the provided columns.
func (m *MaterializedViewBuilder) WhereNotNull(cols ...string) *MaterializedViewBuilder {
	for _, c := range cols {
		m.where = append(m.where, quoteIdent(c)+" IS NOT NULL")
	}
	return m
}

// PartitionKey sets the MV partition key.
func (m *MaterializedViewBuilder) PartitionKey(names ...string) *MaterializedViewBuilder {
	m.pk.Partition = append(m.pk.Partition, names...)
	return m
}

// ClusteringKey sets the MV clustering key.
func (m *MaterializedViewBuilder) ClusteringKey(names ...string) *MaterializedViewBuilder {
	m.pk.Clustering = append(m.pk.Clustering, names...)
	return m
}

// ClusteringOrder sets CLUSTERING ORDER BY entries.
func (m *MaterializedViewBuilder) ClusteringOrder(column string, order ClusteringOrder) *MaterializedViewBuilder {
	m.orderBy = append(m.orderBy, Column{Name: column, Order: order})
	return m
}

// Options attaches table-level options.
func (m *MaterializedViewBuilder) Options(opts TableOptions) *MaterializedViewBuilder {
	m.options = opts
	return m
}

func (m *MaterializedViewBuilder) viewRef() string {
	if m.keyspace != "" {
		return quoteIdent(m.keyspace) + "." + quoteIdent(m.name)
	}
	return quoteIdent(m.name)
}

func (m *MaterializedViewBuilder) baseRef() string {
	if m.baseKeyspace != "" {
		return quoteIdent(m.baseKeyspace) + "." + quoteIdent(m.baseTable)
	}
	return quoteIdent(m.baseTable)
}

// CreateCQL renders CREATE MATERIALIZED VIEW.
func (m *MaterializedViewBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE MATERIALIZED VIEW ")
	if m.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(m.viewRef())
	sb.WriteString(" AS\nSELECT ")
	if len(m.columns) == 0 {
		sb.WriteString("*")
	} else {
		quoted := make([]string, 0, len(m.columns))
		for _, c := range m.columns {
			if c == "*" {
				quoted = append(quoted, "*")
			} else {
				quoted = append(quoted, quoteIdent(c))
			}
		}
		sb.WriteString(strings.Join(quoted, ", "))
	}
	sb.WriteString("\nFROM ")
	sb.WriteString(m.baseRef())
	if len(m.where) > 0 {
		sb.WriteString("\nWHERE ")
		sb.WriteString(strings.Join(m.where, " AND "))
	}
	if pk := m.pk.ToCQL(); pk != "" {
		sb.WriteString("\n")
		sb.WriteString(pk)
	}
	tb := &TableBuilder{options: m.options, orderBy: m.orderBy}
	parts := tb.buildWithParts()
	if len(parts) > 0 {
		sb.WriteString("\n WITH ")
		sb.WriteString(strings.Join(parts, "\n  AND "))
	}
	return sb.String()
}

// DropCQL renders DROP MATERIALIZED VIEW IF EXISTS.
func (m *MaterializedViewBuilder) DropCQL(ifExists bool) string {
	if ifExists {
		return fmt.Sprintf("DROP MATERIALIZED VIEW IF EXISTS %s", m.viewRef())
	}
	return fmt.Sprintf("DROP MATERIALIZED VIEW %s", m.viewRef())
}

// Create executes the CREATE MATERIALIZED VIEW statement.
func (m *MaterializedViewBuilder) Create(ctx context.Context) error {
	return m.db.Exec(ctx, m.CreateCQL())
}

// Drop executes DROP MATERIALIZED VIEW IF EXISTS.
func (m *MaterializedViewBuilder) Drop(ctx context.Context) error {
	return m.db.Exec(ctx, m.DropCQL(true))
}
