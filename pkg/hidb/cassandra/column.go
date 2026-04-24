package cassandra

import (
	"fmt"
	"strings"
)

// ColumnKind classifies a column's role in the primary key.
type ColumnKind int

const (
	// ColumnRegular is a non-key column.
	ColumnRegular ColumnKind = iota
	// ColumnPartitionKey is a partition key component.
	ColumnPartitionKey
	// ColumnClustering is a clustering key component.
	ColumnClustering
	// ColumnStatic is a column shared across rows within a partition.
	ColumnStatic
)

// ClusteringOrder is ASC or DESC on a clustering column.
type ClusteringOrder string

const (
	Asc  ClusteringOrder = "ASC"
	Desc ClusteringOrder = "DESC"
)

// Column describes a single column in a table.
type Column struct {
	Name  string
	Type  DataType
	Kind  ColumnKind
	Order ClusteringOrder // only meaningful for ColumnClustering
	// Position is the ordering of partition / clustering keys.
	// Lower values come first. Regular columns ignore this.
	Position int
}

// Def renders the column definition line: "name type [STATIC]".
func (c Column) Def() string {
	var sb strings.Builder
	sb.WriteString(quoteIdent(c.Name))
	sb.WriteString(" ")
	sb.WriteString(string(c.Type))
	if c.Kind == ColumnStatic {
		sb.WriteString(" STATIC")
	}
	return sb.String()
}

// PrimaryKey holds partition and clustering key column names, in order.
type PrimaryKey struct {
	Partition  []string // one or more columns
	Clustering []string // zero or more columns
}

// ToCQL renders PRIMARY KEY (...) clause contents.
func (p PrimaryKey) ToCQL() string {
	var sb strings.Builder
	if len(p.Partition) == 0 {
		return ""
	}
	sb.WriteString("PRIMARY KEY (")
	if len(p.Partition) == 1 {
		sb.WriteString(quoteIdent(p.Partition[0]))
	} else {
		sb.WriteString("(")
		sb.WriteString(joinQuoted(p.Partition, ", "))
		sb.WriteString(")")
	}
	if len(p.Clustering) > 0 {
		sb.WriteString(", ")
		sb.WriteString(joinQuoted(p.Clustering, ", "))
	}
	sb.WriteString(")")
	return sb.String()
}

// quoteIdent safely quotes an identifier if it contains non-lowercase chars
// or reserved characters. Case-sensitive identifiers must be quoted.
func quoteIdent(name string) string {
	if name == "" {
		return name
	}
	if needsQuoting(name) {
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
	return name
}

func needsQuoting(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return true
		}
	}
	return false
}

func joinQuoted(parts []string, sep string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = quoteIdent(p)
	}
	return strings.Join(quoted, sep)
}

// cqlLiteral renders a Go value as a CQL literal, used for constant option
// values such as default_time_to_live. Only primitive scalars are supported;
// complex values should be passed via the ? placeholder path.
func cqlLiteral(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case float32, float64:
		return fmt.Sprintf("%g", x)
	default:
		return fmt.Sprintf("%v", x)
	}
}
