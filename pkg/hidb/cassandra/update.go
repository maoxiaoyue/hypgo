package cassandra

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/gocql/gocql"
)

// UpdateBuilder builds UPDATE statements.
type UpdateBuilder struct {
	db          *CassandraDB
	keyspace    string
	table       string
	assignments []assignment
	wheres      []whereClause
	ifExists    bool
	ifCond      []whereClause
	ttl         int
	timestamp   int64
	consistency *gocql.Consistency
}

type assignment struct {
	expr string
	args []interface{}
}

// Update starts an UPDATE builder.
func (c *CassandraDB) Update(table string) *UpdateBuilder {
	ks, t := splitQualified(table)
	return &UpdateBuilder{db: c, keyspace: ks, table: t}
}

// Set assigns col = value.
func (u *UpdateBuilder) Set(col string, v interface{}) *UpdateBuilder {
	u.assignments = append(u.assignments, assignment{expr: quoteIdent(col) + " = ?", args: []interface{}{v}})
	return u
}

// SetExpr adds a raw assignment expression, e.g. "counter = counter + ?".
func (u *UpdateBuilder) SetExpr(expr string, args ...interface{}) *UpdateBuilder {
	u.assignments = append(u.assignments, assignment{expr: expr, args: args})
	return u
}

// Increment is a convenience for counter columns: counter = counter + n.
func (u *UpdateBuilder) Increment(col string, delta int64) *UpdateBuilder {
	return u.SetExpr(fmt.Sprintf("%s = %s + ?", quoteIdent(col), quoteIdent(col)), delta)
}

// Decrement is a convenience for counter columns: counter = counter - n.
func (u *UpdateBuilder) Decrement(col string, delta int64) *UpdateBuilder {
	return u.SetExpr(fmt.Sprintf("%s = %s - ?", quoteIdent(col), quoteIdent(col)), delta)
}

// Append appends to a list column: list = list + ?.
func (u *UpdateBuilder) Append(col string, values interface{}) *UpdateBuilder {
	return u.SetExpr(fmt.Sprintf("%s = %s + ?", quoteIdent(col), quoteIdent(col)), values)
}

// Prepend prepends to a list column: list = ? + list.
func (u *UpdateBuilder) Prepend(col string, values interface{}) *UpdateBuilder {
	return u.SetExpr(fmt.Sprintf("%s = ? + %s", quoteIdent(col), quoteIdent(col)), values)
}

// Remove removes elements from a set/list: col = col - ?.
func (u *UpdateBuilder) Remove(col string, values interface{}) *UpdateBuilder {
	return u.SetExpr(fmt.Sprintf("%s = %s - ?", quoteIdent(col), quoteIdent(col)), values)
}

// Where appends a predicate.
func (u *UpdateBuilder) Where(expr string, args ...interface{}) *UpdateBuilder {
	u.wheres = append(u.wheres, whereClause{expr: expr, args: args})
	return u
}

// WhereEq is a convenience for col = ?.
func (u *UpdateBuilder) WhereEq(col string, v interface{}) *UpdateBuilder {
	return u.Where(quoteIdent(col)+" = ?", v)
}

// If adds an IF ... conditional (LWT).
func (u *UpdateBuilder) If(expr string, args ...interface{}) *UpdateBuilder {
	u.ifCond = append(u.ifCond, whereClause{expr: expr, args: args})
	return u
}

// IfExists adds IF EXISTS (LWT).
func (u *UpdateBuilder) IfExists() *UpdateBuilder { u.ifExists = true; return u }

// TTL sets USING TTL.
func (u *UpdateBuilder) TTL(seconds int) *UpdateBuilder { u.ttl = seconds; return u }

// Timestamp sets USING TIMESTAMP.
func (u *UpdateBuilder) Timestamp(us int64) *UpdateBuilder { u.timestamp = us; return u }

// Consistency overrides consistency.
func (u *UpdateBuilder) Consistency(cl gocql.Consistency) *UpdateBuilder {
	u.consistency = &cl
	return u
}

func (u *UpdateBuilder) tableRef() string {
	if u.keyspace != "" {
		return quoteIdent(u.keyspace) + "." + quoteIdent(u.table)
	}
	return quoteIdent(u.table)
}

// CQL renders the UPDATE statement.
func (u *UpdateBuilder) CQL() (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	sb.WriteString("UPDATE ")
	sb.WriteString(u.tableRef())
	using := make([]string, 0, 2)
	if u.ttl > 0 {
		using = append(using, fmt.Sprintf("TTL %d", u.ttl))
	}
	if u.timestamp > 0 {
		using = append(using, fmt.Sprintf("TIMESTAMP %d", u.timestamp))
	}
	if len(using) > 0 {
		sb.WriteString(" USING ")
		sb.WriteString(strings.Join(using, " AND "))
	}
	sb.WriteString(" SET ")
	setParts := make([]string, 0, len(u.assignments))
	for _, a := range u.assignments {
		setParts = append(setParts, a.expr)
		args = append(args, a.args...)
	}
	sb.WriteString(strings.Join(setParts, ", "))
	if len(u.wheres) > 0 {
		sb.WriteString(" WHERE ")
		whereParts := make([]string, 0, len(u.wheres))
		for _, w := range u.wheres {
			whereParts = append(whereParts, w.expr)
			args = append(args, w.args...)
		}
		sb.WriteString(strings.Join(whereParts, " AND "))
	}
	if u.ifExists {
		sb.WriteString(" IF EXISTS")
	} else if len(u.ifCond) > 0 {
		sb.WriteString(" IF ")
		condParts := make([]string, 0, len(u.ifCond))
		for _, c := range u.ifCond {
			condParts = append(condParts, c.expr)
			args = append(args, c.args...)
		}
		sb.WriteString(strings.Join(condParts, " AND "))
	}
	return sb.String(), args
}

// Exec runs the UPDATE.
func (u *UpdateBuilder) Exec(ctx context.Context) error {
	stmt, args := u.CQL()
	q := u.db.session.Query(stmt, args...).WithContext(ctx)
	if u.consistency != nil {
		q = q.Consistency(*u.consistency)
	}
	return q.Exec()
}

// ExecCAS executes as lightweight transaction.
func (u *UpdateBuilder) ExecCAS(ctx context.Context, dest ...interface{}) (bool, error) {
	stmt, args := u.CQL()
	q := u.db.session.Query(stmt, args...).WithContext(ctx)
	if u.consistency != nil {
		q = q.Consistency(*u.consistency)
	}
	return q.ScanCAS(dest...)
}

// UpdateModel generates an UPDATE from a model using its primary key fields
// as the WHERE clause. Non-key non-counter fields are written to the SET
// clause (respecting omitempty).
func (c *CassandraDB) UpdateModel(ctx context.Context, model interface{}, opts ...UpdateOption) error {
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
	ub := c.Update(name)
	for _, f := range info.Fields {
		v := rv.FieldByIndex(f.Index).Interface()
		switch f.Kind {
		case ColumnPartitionKey, ColumnClustering:
			ub.WhereEq(f.Name, v)
		default:
			if f.Counter {
				continue
			}
			if f.OmitEmpty && isZero(v) {
				continue
			}
			ub.Set(f.Name, v)
		}
	}
	for _, opt := range opts {
		opt(ub)
	}
	return ub.Exec(ctx)
}

// UpdateOption customises UpdateModel.
type UpdateOption func(*UpdateBuilder)

// UpdateTTL sets USING TTL.
func UpdateTTL(seconds int) UpdateOption { return func(u *UpdateBuilder) { u.ttl = seconds } }

// UpdateTimestamp sets USING TIMESTAMP.
func UpdateTimestamp(us int64) UpdateOption { return func(u *UpdateBuilder) { u.timestamp = us } }

// UpdateConsistency sets consistency.
func UpdateConsistency(cl gocql.Consistency) UpdateOption {
	return func(u *UpdateBuilder) { u.consistency = &cl }
}

// UpdateIfExists adds IF EXISTS.
func UpdateIfExists() UpdateOption { return func(u *UpdateBuilder) { u.ifExists = true } }
