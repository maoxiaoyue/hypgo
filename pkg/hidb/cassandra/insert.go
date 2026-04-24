package cassandra

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

// InsertBuilder builds INSERT statements.
type InsertBuilder struct {
	db          *CassandraDB
	keyspace    string
	table       string
	columns     []string
	values      []interface{}
	ifNotExist  bool
	ttl         int
	timestamp   int64
	consistency *gocql.Consistency
}

// Insert starts an INSERT builder.
func (c *CassandraDB) Insert(table string) *InsertBuilder {
	ks, t := splitQualified(table)
	return &InsertBuilder{db: c, keyspace: ks, table: t}
}

// Value appends a single column/value pair.
func (i *InsertBuilder) Value(col string, val interface{}) *InsertBuilder {
	i.columns = append(i.columns, col)
	i.values = append(i.values, val)
	return i
}

// Values appends a map of columns (ordering is undefined; use Value for deterministic ordering).
func (i *InsertBuilder) Values(m map[string]interface{}) *InsertBuilder {
	for k, v := range m {
		i.Value(k, v)
	}
	return i
}

// IfNotExists toggles IF NOT EXISTS (lightweight transaction).
func (i *InsertBuilder) IfNotExists() *InsertBuilder { i.ifNotExist = true; return i }

// TTL sets USING TTL (seconds).
func (i *InsertBuilder) TTL(seconds int) *InsertBuilder { i.ttl = seconds; return i }

// Timestamp sets USING TIMESTAMP (microseconds).
func (i *InsertBuilder) Timestamp(us int64) *InsertBuilder { i.timestamp = us; return i }

// Consistency overrides consistency.
func (i *InsertBuilder) Consistency(cl gocql.Consistency) *InsertBuilder {
	i.consistency = &cl
	return i
}

func (i *InsertBuilder) tableRef() string {
	if i.keyspace != "" {
		return quoteIdent(i.keyspace) + "." + quoteIdent(i.table)
	}
	return quoteIdent(i.table)
}

// CQL renders the INSERT statement and its bind args.
func (i *InsertBuilder) CQL() (string, []interface{}) {
	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(i.tableRef())
	sb.WriteString(" (")
	sb.WriteString(joinQuoted(i.columns, ", "))
	sb.WriteString(") VALUES (")
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(i.values)), ", ")
	sb.WriteString(placeholders)
	sb.WriteString(")")
	if i.ifNotExist {
		sb.WriteString(" IF NOT EXISTS")
	}
	using := make([]string, 0, 2)
	if i.ttl > 0 {
		using = append(using, fmt.Sprintf("TTL %d", i.ttl))
	}
	if i.timestamp > 0 {
		using = append(using, fmt.Sprintf("TIMESTAMP %d", i.timestamp))
	}
	if len(using) > 0 {
		sb.WriteString(" USING ")
		sb.WriteString(strings.Join(using, " AND "))
	}
	return sb.String(), i.values
}

// Exec runs the INSERT.
func (i *InsertBuilder) Exec(ctx context.Context) error {
	stmt, args := i.CQL()
	q := i.db.session.Query(stmt, args...).WithContext(ctx)
	if i.consistency != nil {
		q = q.Consistency(*i.consistency)
	}
	return q.Exec()
}

// ExecCAS executes a lightweight transaction and returns applied + existing row.
func (i *InsertBuilder) ExecCAS(ctx context.Context, dest ...interface{}) (bool, error) {
	stmt, args := i.CQL()
	q := i.db.session.Query(stmt, args...).WithContext(ctx)
	if i.consistency != nil {
		q = q.Consistency(*i.consistency)
	}
	return q.ScanCAS(dest...)
}

// Save inserts a model struct. Zero-valued fields tagged with omitempty are
// skipped so Cassandra's tombstone-avoidance semantics are preserved.
func (c *CassandraDB) Save(ctx context.Context, model interface{}, opts ...SaveOption) error {
	info, err := ParseModel(model)
	if err != nil {
		return err
	}
	rv := reflect.ValueOf(model)
	for rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	name := info.Table
	if info.Keyspace != "" {
		name = info.Keyspace + "." + info.Table
	}
	ins := c.Insert(name)
	for _, f := range info.Fields {
		v := rv.FieldByIndex(f.Index).Interface()
		if f.OmitEmpty && isZero(v) {
			continue
		}
		ins.Value(f.Name, v)
	}
	for _, opt := range opts {
		opt(ins)
	}
	return ins.Exec(ctx)
}

// SaveOption customises an insert built from a model.
type SaveOption func(*InsertBuilder)

// SaveIfNotExists adds IF NOT EXISTS.
func SaveIfNotExists() SaveOption {
	return func(i *InsertBuilder) { i.ifNotExist = true }
}

// SaveTTL sets USING TTL.
func SaveTTL(seconds int) SaveOption {
	return func(i *InsertBuilder) { i.ttl = seconds }
}

// SaveTimestamp sets USING TIMESTAMP.
func SaveTimestamp(us int64) SaveOption {
	return func(i *InsertBuilder) { i.timestamp = us }
}

// SaveConsistency overrides consistency.
func SaveConsistency(cl gocql.Consistency) SaveOption {
	return func(i *InsertBuilder) { i.consistency = &cl }
}

func isZero(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface:
		return rv.IsNil()
	case reflect.Slice, reflect.Map:
		return rv.Len() == 0
	}
	if t, ok := v.(time.Time); ok {
		return t.IsZero()
	}
	z := reflect.Zero(rv.Type()).Interface()
	return reflect.DeepEqual(v, z)
}
