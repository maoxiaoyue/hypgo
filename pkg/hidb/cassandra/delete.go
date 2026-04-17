package cassandra

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/gocql/gocql"
)

// DeleteBuilder builds DELETE statements.
type DeleteBuilder struct {
	db          *CassandraDB
	keyspace    string
	table       string
	columns     []string // deletes specific columns if non-empty
	wheres      []whereClause
	ifExists    bool
	ifCond      []whereClause
	timestamp   int64
	consistency *gocql.Consistency
}

// Delete starts a DELETE builder.
func (c *CassandraDB) Delete(table string) *DeleteBuilder {
	ks, t := splitQualified(table)
	return &DeleteBuilder{db: c, keyspace: ks, table: t}
}

// Columns restricts the DELETE to specific columns/elements.
// Use e.g. `tags[3]` to delete a map/list element, or a bare column name to
// null it out.
func (d *DeleteBuilder) Columns(cols ...string) *DeleteBuilder {
	d.columns = append(d.columns, cols...)
	return d
}

// Where appends a predicate.
func (d *DeleteBuilder) Where(expr string, args ...interface{}) *DeleteBuilder {
	d.wheres = append(d.wheres, whereClause{expr: expr, args: args})
	return d
}

// WhereEq is a convenience for col = ?.
func (d *DeleteBuilder) WhereEq(col string, v interface{}) *DeleteBuilder {
	return d.Where(quoteIdent(col)+" = ?", v)
}

// If adds an IF condition.
func (d *DeleteBuilder) If(expr string, args ...interface{}) *DeleteBuilder {
	d.ifCond = append(d.ifCond, whereClause{expr: expr, args: args})
	return d
}

// IfExists adds IF EXISTS.
func (d *DeleteBuilder) IfExists() *DeleteBuilder { d.ifExists = true; return d }

// Timestamp sets USING TIMESTAMP.
func (d *DeleteBuilder) Timestamp(us int64) *DeleteBuilder { d.timestamp = us; return d }

// Consistency overrides consistency.
func (d *DeleteBuilder) Consistency(cl gocql.Consistency) *DeleteBuilder {
	d.consistency = &cl
	return d
}

func (d *DeleteBuilder) tableRef() string {
	if d.keyspace != "" {
		return quoteIdent(d.keyspace) + "." + quoteIdent(d.table)
	}
	return quoteIdent(d.table)
}

// CQL renders the DELETE statement.
func (d *DeleteBuilder) CQL() (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	sb.WriteString("DELETE ")
	if len(d.columns) > 0 {
		parts := make([]string, len(d.columns))
		for i, c := range d.columns {
			// If the column references a collection element (contains '[' and ']')
			// preserve it literally; otherwise quote.
			if strings.ContainsAny(c, "[]") {
				parts[i] = c
			} else {
				parts[i] = quoteIdent(c)
			}
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString(" ")
	}
	sb.WriteString("FROM ")
	sb.WriteString(d.tableRef())
	if d.timestamp > 0 {
		fmt.Fprintf(&sb, " USING TIMESTAMP %d", d.timestamp)
	}
	if len(d.wheres) > 0 {
		sb.WriteString(" WHERE ")
		whereParts := make([]string, 0, len(d.wheres))
		for _, w := range d.wheres {
			whereParts = append(whereParts, w.expr)
			args = append(args, w.args...)
		}
		sb.WriteString(strings.Join(whereParts, " AND "))
	}
	if d.ifExists {
		sb.WriteString(" IF EXISTS")
	} else if len(d.ifCond) > 0 {
		sb.WriteString(" IF ")
		condParts := make([]string, 0, len(d.ifCond))
		for _, c := range d.ifCond {
			condParts = append(condParts, c.expr)
			args = append(args, c.args...)
		}
		sb.WriteString(strings.Join(condParts, " AND "))
	}
	return sb.String(), args
}

// Exec runs the DELETE.
func (d *DeleteBuilder) Exec(ctx context.Context) error {
	stmt, args := d.CQL()
	q := d.db.session.Query(stmt, args...).WithContext(ctx)
	if d.consistency != nil {
		q = q.Consistency(*d.consistency)
	}
	return q.Exec()
}

// ExecCAS executes as lightweight transaction.
func (d *DeleteBuilder) ExecCAS(ctx context.Context, dest ...interface{}) (bool, error) {
	stmt, args := d.CQL()
	q := d.db.session.Query(stmt, args...).WithContext(ctx)
	if d.consistency != nil {
		q = q.Consistency(*d.consistency)
	}
	return q.ScanCAS(dest...)
}

// DeleteModel deletes the row identified by the model's primary key.
func (c *CassandraDB) DeleteModel(ctx context.Context, model interface{}) error {
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
	db := c.Delete(name)
	for _, f := range info.Fields {
		switch f.Kind {
		case ColumnPartitionKey, ColumnClustering:
			db.WhereEq(f.Name, rv.FieldByIndex(f.Index).Interface())
		}
	}
	return db.Exec(ctx)
}
