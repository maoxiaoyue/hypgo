package cassandra

import (
	"context"
	"fmt"
	"strings"
)

// DescribeTarget enumerates the schema object kinds DESCRIBE can introspect.
type DescribeTarget string

const (
	DescKeyspaces       DescribeTarget = "KEYSPACES"
	DescKeyspace        DescribeTarget = "KEYSPACE"
	DescTables          DescribeTarget = "TABLES"
	DescTable           DescribeTarget = "TABLE"
	DescTypes           DescribeTarget = "TYPES"
	DescType            DescribeTarget = "TYPE"
	DescFunctions       DescribeTarget = "FUNCTIONS"
	DescFunction        DescribeTarget = "FUNCTION"
	DescAggregates      DescribeTarget = "AGGREGATES"
	DescAggregate       DescribeTarget = "AGGREGATE"
	DescIndex           DescribeTarget = "INDEX"
	DescMaterialized    DescribeTarget = "MATERIALIZED VIEW"
	DescCluster         DescribeTarget = "CLUSTER"
	DescSchema          DescribeTarget = "SCHEMA"
	DescFullSchema      DescribeTarget = "FULL SCHEMA"
)

// DescribeCQL renders a DESCRIBE statement.
// target: the kind of object (use DescKeyspace, DescTable, etc.).
// name: optional identifier for singular targets; may be "ks.obj" for qualified names.
func DescribeCQL(target DescribeTarget, name string) string {
	t := string(target)
	if name == "" {
		return "DESCRIBE " + t
	}
	return "DESCRIBE " + t + " " + qualifyIdent(name)
}

// qualifyIdent quotes each dotted segment ("ks.table" -> "ks"."table").
func qualifyIdent(name string) string {
	if name == "" {
		return ""
	}
	parts := strings.SplitN(name, ".", 2)
	for i, p := range parts {
		parts[i] = quoteIdent(p)
	}
	return strings.Join(parts, ".")
}

// ===== High-level introspection via system_schema =====

// KeyspaceInfo is the subset of system_schema.keyspaces commonly needed.
type KeyspaceInfo struct {
	Name          string
	Replication   map[string]string
	DurableWrites bool
}

// TableInfo is the subset of system_schema.tables commonly needed.
type TableInfo struct {
	Keyspace string
	Name     string
	Comment  string
	Options  map[string]string // raw string options (flattened)
}

// ColumnInfo describes one column from system_schema.columns.
type ColumnInfo struct {
	Keyspace        string
	Table           string
	Column          string
	Kind            string // partition_key / clustering / regular / static
	Position        int
	Type            string
	ClusteringOrder string
}

// DescribeKeyspaces lists all keyspaces via system_schema.keyspaces.
func (c *CassandraDB) DescribeKeyspaces(ctx context.Context) ([]KeyspaceInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	iter := c.session.Query(
		`SELECT keyspace_name, replication, durable_writes FROM system_schema.keyspaces`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []KeyspaceInfo
	var name string
	var repl map[string]string
	var durable bool
	for iter.Scan(&name, &repl, &durable) {
		out = append(out, KeyspaceInfo{Name: name, Replication: repl, DurableWrites: durable})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// DescribeKeyspace returns the KeyspaceInfo for a single keyspace.
func (c *CassandraDB) DescribeKeyspace(ctx context.Context, ks string) (*KeyspaceInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	var name string
	var repl map[string]string
	var durable bool
	err := c.session.Query(
		`SELECT keyspace_name, replication, durable_writes FROM system_schema.keyspaces WHERE keyspace_name = ?`,
		ks,
	).WithContext(ctx).Scan(&name, &repl, &durable)
	if err != nil {
		return nil, err
	}
	return &KeyspaceInfo{Name: name, Replication: repl, DurableWrites: durable}, nil
}

// DescribeTables lists all tables in the given keyspace (empty = session keyspace).
func (c *CassandraDB) DescribeTables(ctx context.Context, keyspace string) ([]TableInfo, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}
	ks := keyspace
	if ks == "" {
		ks = c.config.Keyspace
	}
	if ks == "" {
		return nil, fmt.Errorf("cassandra: keyspace required")
	}
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, comment FROM system_schema.tables WHERE keyspace_name = ?`,
		ks,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []TableInfo
	var kn, tn, comment string
	for iter.Scan(&kn, &tn, &comment) {
		out = append(out, TableInfo{Keyspace: kn, Name: tn, Comment: comment})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// DescribeColumns returns the columns of a single table.
func (c *CassandraDB) DescribeColumns(ctx context.Context, keyspace, table string) ([]ColumnInfo, error) {
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
		`SELECT keyspace_name, table_name, column_name, kind, position, type, clustering_order
		 FROM system_schema.columns WHERE keyspace_name = ? AND table_name = ?`,
		ks, table,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []ColumnInfo
	var kn, tn, cn, kind, typ, order string
	var pos int
	for iter.Scan(&kn, &tn, &cn, &kind, &pos, &typ, &order) {
		out = append(out, ColumnInfo{
			Keyspace: kn, Table: tn, Column: cn, Kind: kind,
			Position: pos, Type: typ, ClusteringOrder: order,
		})
	}
	if err := iter.Close(); err != nil {
		return nil, err
	}
	return out, nil
}

// DescribeTable returns the TableInfo plus columns.
func (c *CassandraDB) DescribeTable(ctx context.Context, keyspace, table string) (*TableInfo, []ColumnInfo, error) {
	ks := keyspace
	if ks == "" {
		ks = c.config.Keyspace
	}
	if ks == "" || table == "" {
		return nil, nil, fmt.Errorf("cassandra: keyspace and table are required")
	}
	var kn, tn, comment string
	err := c.session.Query(
		`SELECT keyspace_name, table_name, comment FROM system_schema.tables
		 WHERE keyspace_name = ? AND table_name = ?`,
		ks, table,
	).WithContext(ctx).Scan(&kn, &tn, &comment)
	if err != nil {
		return nil, nil, err
	}
	cols, err := c.DescribeColumns(ctx, ks, table)
	if err != nil {
		return nil, nil, err
	}
	return &TableInfo{Keyspace: kn, Name: tn, Comment: comment}, cols, nil
}

// RenderTableDDL reconstructs an approximate CREATE TABLE statement from
// system_schema columns. Intended for display / debugging; it preserves
// partition/clustering ordering but omits most storage-engine options.
func RenderTableDDL(tbl TableInfo, cols []ColumnInfo) string {
	var partition, clustering, regular []ColumnInfo
	for _, c := range cols {
		switch c.Kind {
		case "partition_key":
			partition = append(partition, c)
		case "clustering":
			clustering = append(clustering, c)
		default:
			regular = append(regular, c)
		}
	}
	// stable sort by position for key parts
	sortByPos := func(xs []ColumnInfo) {
		for i := 1; i < len(xs); i++ {
			for j := i; j > 0 && xs[j-1].Position > xs[j].Position; j-- {
				xs[j-1], xs[j] = xs[j], xs[j-1]
			}
		}
	}
	sortByPos(partition)
	sortByPos(clustering)

	var sb strings.Builder
	sb.WriteString("CREATE TABLE ")
	if tbl.Keyspace != "" {
		sb.WriteString(quoteIdent(tbl.Keyspace))
		sb.WriteString(".")
	}
	sb.WriteString(quoteIdent(tbl.Name))
	sb.WriteString(" (\n")
	all := make([]ColumnInfo, 0, len(cols))
	all = append(all, partition...)
	all = append(all, clustering...)
	all = append(all, regular...)
	for i, c := range all {
		sb.WriteString("  ")
		sb.WriteString(quoteIdent(c.Column))
		sb.WriteString(" ")
		sb.WriteString(c.Type)
		if i < len(all)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	// primary key
	sb.WriteString("  PRIMARY KEY (")
	if len(partition) > 1 {
		sb.WriteString("(")
		for i, p := range partition {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(quoteIdent(p.Column))
		}
		sb.WriteString(")")
	} else if len(partition) == 1 {
		sb.WriteString(quoteIdent(partition[0].Column))
	}
	for _, c := range clustering {
		sb.WriteString(", ")
		sb.WriteString(quoteIdent(c.Column))
	}
	sb.WriteString(")\n)")
	if len(clustering) > 0 {
		sb.WriteString(" WITH CLUSTERING ORDER BY (")
		for i, c := range clustering {
			if i > 0 {
				sb.WriteString(", ")
			}
			order := c.ClusteringOrder
			if order == "" {
				order = "ASC"
			}
			sb.WriteString(quoteIdent(c.Column))
			sb.WriteString(" ")
			sb.WriteString(strings.ToUpper(order))
		}
		sb.WriteString(")")
	}
	if tbl.Comment != "" {
		if len(clustering) > 0 {
			sb.WriteString(" AND ")
		} else {
			sb.WriteString(" WITH ")
		}
		sb.WriteString("comment = '")
		sb.WriteString(strings.ReplaceAll(tbl.Comment, "'", "''"))
		sb.WriteString("'")
	}
	return sb.String()
}
