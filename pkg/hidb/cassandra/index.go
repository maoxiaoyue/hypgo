package cassandra

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// IndexKind selects the kind of secondary index.
type IndexKind int

const (
	// IndexRegular is the default 2i secondary index.
	IndexRegular IndexKind = iota
	// IndexSAI is a Storage-Attached Index (Cassandra 5.0+).
	IndexSAI
	// IndexCustom uses a user-specified implementation class.
	IndexCustom
)

// IndexTarget describes which part of a column is indexed.
// Only applies to collections.
type IndexTarget string

const (
	IndexTargetValues  IndexTarget = "VALUES"
	IndexTargetKeys    IndexTarget = "KEYS"
	IndexTargetEntries IndexTarget = "ENTRIES"
	IndexTargetFull    IndexTarget = "FULL"
)

// IndexBuilder builds CREATE INDEX / DROP INDEX statements.
type IndexBuilder struct {
	db         *CassandraDB
	keyspace   string
	table      string
	name       string
	column     string
	kind       IndexKind
	target     IndexTarget
	customCls  string
	options    map[string]interface{}
	ifNotExist bool
}

// Index creates a builder targeting indexName on table.column.
// Pass empty indexName to let Cassandra pick one.
func (c *CassandraDB) Index(name string) *IndexBuilder {
	return &IndexBuilder{db: c, name: name, ifNotExist: true}
}

// IfNotExists toggles IF NOT EXISTS (default true).
func (i *IndexBuilder) IfNotExists(v bool) *IndexBuilder {
	i.ifNotExist = v
	return i
}

// On targets table.column. Table may be "ks.table".
func (i *IndexBuilder) On(table, column string) *IndexBuilder {
	ks, tbl := splitQualified(table)
	i.keyspace = ks
	i.table = tbl
	i.column = column
	return i
}

// SAI selects a Storage-Attached Index (recommended in 5.0).
func (i *IndexBuilder) SAI() *IndexBuilder {
	i.kind = IndexSAI
	return i
}

// Custom selects a user-specified class implementation.
func (i *IndexBuilder) Custom(class string) *IndexBuilder {
	i.kind = IndexCustom
	i.customCls = class
	return i
}

// Target sets the indexed portion of a collection column.
func (i *IndexBuilder) Target(t IndexTarget) *IndexBuilder {
	i.target = t
	return i
}

// Options sets extra WITH OPTIONS for the index (SAI accepts similarity_function,
// case_sensitive, normalize, ascii etc).
func (i *IndexBuilder) Options(opts map[string]interface{}) *IndexBuilder {
	if i.options == nil {
		i.options = make(map[string]interface{}, len(opts))
	}
	for k, v := range opts {
		i.options[k] = v
	}
	return i
}

// Option sets a single WITH OPTIONS entry.
func (i *IndexBuilder) Option(key string, value interface{}) *IndexBuilder {
	if i.options == nil {
		i.options = make(map[string]interface{})
	}
	i.options[key] = value
	return i
}

func (i *IndexBuilder) tableRef() string {
	if i.keyspace != "" {
		return quoteIdent(i.keyspace) + "." + quoteIdent(i.table)
	}
	return quoteIdent(i.table)
}

// CreateCQL renders the CREATE INDEX statement.
func (i *IndexBuilder) CreateCQL() string {
	var sb strings.Builder
	switch i.kind {
	case IndexCustom:
		sb.WriteString("CREATE CUSTOM INDEX ")
	case IndexSAI:
		sb.WriteString("CREATE INDEX ")
	default:
		sb.WriteString("CREATE INDEX ")
	}
	if i.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	if i.name != "" {
		sb.WriteString(quoteIdent(i.name))
		sb.WriteString(" ")
	}
	sb.WriteString("ON ")
	sb.WriteString(i.tableRef())
	sb.WriteString(" (")
	if i.target != "" {
		fmt.Fprintf(&sb, "%s(%s)", i.target, quoteIdent(i.column))
	} else {
		sb.WriteString(quoteIdent(i.column))
	}
	sb.WriteString(")")

	switch i.kind {
	case IndexSAI:
		sb.WriteString(" USING 'sai'")
	case IndexCustom:
		fmt.Fprintf(&sb, " USING '%s'", i.customCls)
	}

	if len(i.options) > 0 {
		sb.WriteString(" WITH OPTIONS = {")
		keys := make([]string, 0, len(i.options))
		for k := range i.options {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("'%s': %s", k, quoteOptionValue(i.options[k])))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("}")
	}
	return sb.String()
}

// DropCQL renders DROP INDEX IF EXISTS <name>.
func (i *IndexBuilder) DropCQL(ifExists bool) string {
	ref := quoteIdent(i.name)
	if i.keyspace != "" {
		ref = quoteIdent(i.keyspace) + "." + ref
	}
	if ifExists {
		return "DROP INDEX IF EXISTS " + ref
	}
	return "DROP INDEX " + ref
}

// Create executes the CREATE INDEX statement.
func (i *IndexBuilder) Create(ctx context.Context) error {
	return i.db.Exec(ctx, i.CreateCQL())
}

// Drop executes DROP INDEX IF EXISTS.
func (i *IndexBuilder) Drop(ctx context.Context) error {
	return i.db.Exec(ctx, i.DropCQL(true))
}
