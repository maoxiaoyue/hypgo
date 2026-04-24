package cassandra

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/gocql/gocql"
)

// SelectBuilder builds CQL SELECT statements.
type SelectBuilder struct {
	db             *CassandraDB
	keyspace       string
	table          string
	columns        []string
	wheres         []whereClause
	orderBy        []Column
	limit          int
	perPartition   int
	allowFiltering bool
	distinct       bool
	ann            *annClause
	consistency    *gocql.Consistency
	pageSize       int
	pageState      []byte
}

type whereClause struct {
	expr string
	args []interface{}
}

type annClause struct {
	column string
	vector []float32
	limit  int
}

// Select returns a SELECT builder for the given table.
func (c *CassandraDB) Select(table string, columns ...string) *SelectBuilder {
	ks, t := splitQualified(table)
	if len(columns) == 0 {
		columns = []string{"*"}
	}
	return &SelectBuilder{db: c, keyspace: ks, table: t, columns: columns}
}

// SelectModel is a convenience that reads the table name from a model.
func (c *CassandraDB) SelectModel(model interface{}, columns ...string) (*SelectBuilder, error) {
	info, err := ParseModel(model)
	if err != nil {
		return nil, err
	}
	name := info.Table
	if info.Keyspace != "" {
		name = info.Keyspace + "." + info.Table
	}
	if len(columns) == 0 {
		columns = info.Columns()
	}
	return c.Select(name, columns...), nil
}

// Distinct adds the DISTINCT keyword.
func (s *SelectBuilder) Distinct() *SelectBuilder { s.distinct = true; return s }

// Where appends a predicate. Use ? placeholders; arguments are bound in order.
func (s *SelectBuilder) Where(expr string, args ...interface{}) *SelectBuilder {
	s.wheres = append(s.wheres, whereClause{expr: expr, args: args})
	return s
}

// WhereEq appends a column = ? clause.
func (s *SelectBuilder) WhereEq(col string, v interface{}) *SelectBuilder {
	return s.Where(quoteIdent(col)+" = ?", v)
}

// WhereIn appends a column IN (...) clause.
func (s *SelectBuilder) WhereIn(col string, values ...interface{}) *SelectBuilder {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(values)), ", ")
	expr := fmt.Sprintf("%s IN (%s)", quoteIdent(col), placeholders)
	return s.Where(expr, values...)
}

// OrderBy adds a clustering ORDER BY entry.
func (s *SelectBuilder) OrderBy(col string, order ClusteringOrder) *SelectBuilder {
	s.orderBy = append(s.orderBy, Column{Name: col, Order: order})
	return s
}

// Limit sets the LIMIT clause.
func (s *SelectBuilder) Limit(n int) *SelectBuilder { s.limit = n; return s }

// PerPartitionLimit sets PER PARTITION LIMIT.
func (s *SelectBuilder) PerPartitionLimit(n int) *SelectBuilder { s.perPartition = n; return s }

// AllowFiltering appends ALLOW FILTERING.
func (s *SelectBuilder) AllowFiltering() *SelectBuilder { s.allowFiltering = true; return s }

// ANNOf adds a vector similarity search (Cassandra 5.0 + SAI).
// Produces ORDER BY <col> ANN OF [v1,v2,...] LIMIT <n>.
func (s *SelectBuilder) ANNOf(column string, vector []float32, limit int) *SelectBuilder {
	vec := make([]float32, len(vector))
	copy(vec, vector)
	s.ann = &annClause{column: column, vector: vec, limit: limit}
	return s
}

// Consistency overrides the consistency level for this query.
func (s *SelectBuilder) Consistency(cl gocql.Consistency) *SelectBuilder {
	s.consistency = &cl
	return s
}

// PageSize sets the page size (paging fetch size).
func (s *SelectBuilder) PageSize(n int) *SelectBuilder { s.pageSize = n; return s }

// PageState sets the paging state for cursor-style pagination.
func (s *SelectBuilder) PageState(state []byte) *SelectBuilder {
	s.pageState = state
	return s
}

func (s *SelectBuilder) tableRef() string {
	if s.keyspace != "" {
		return quoteIdent(s.keyspace) + "." + quoteIdent(s.table)
	}
	return quoteIdent(s.table)
}

// CQL renders the SELECT statement and its ordered bind arguments.
func (s *SelectBuilder) CQL() (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}
	sb.WriteString("SELECT ")
	if s.distinct {
		sb.WriteString("DISTINCT ")
	}
	parts := make([]string, 0, len(s.columns))
	for _, c := range s.columns {
		if c == "*" {
			parts = append(parts, "*")
		} else {
			parts = append(parts, quoteIdent(c))
		}
	}
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteString(" FROM ")
	sb.WriteString(s.tableRef())
	if len(s.wheres) > 0 {
		sb.WriteString(" WHERE ")
		exprs := make([]string, 0, len(s.wheres))
		for _, w := range s.wheres {
			exprs = append(exprs, w.expr)
			args = append(args, w.args...)
		}
		sb.WriteString(strings.Join(exprs, " AND "))
	}
	orderParts := make([]string, 0, len(s.orderBy))
	for _, o := range s.orderBy {
		orderParts = append(orderParts, quoteIdent(o.Name)+" "+string(firstOrder(o.Order)))
	}
	if s.ann != nil {
		orderParts = append(orderParts, fmt.Sprintf("%s ANN OF %s", quoteIdent(s.ann.column), formatVectorLiteral(s.ann.vector)))
	}
	if len(orderParts) > 0 {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(strings.Join(orderParts, ", "))
	}
	if s.perPartition > 0 {
		fmt.Fprintf(&sb, " PER PARTITION LIMIT %d", s.perPartition)
	}
	limit := s.limit
	if s.ann != nil && s.ann.limit > 0 && limit == 0 {
		limit = s.ann.limit
	}
	if limit > 0 {
		fmt.Fprintf(&sb, " LIMIT %d", limit)
	}
	if s.allowFiltering {
		sb.WriteString(" ALLOW FILTERING")
	}
	return sb.String(), args
}

func firstOrder(o ClusteringOrder) ClusteringOrder {
	if o == "" {
		return Asc
	}
	return o
}

func formatVectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// query returns a gocql.Query configured with the builder state.
func (s *SelectBuilder) query(ctx context.Context) *gocql.Query {
	stmt, args := s.CQL()
	q := s.db.session.Query(stmt, args...).WithContext(ctx)
	if s.consistency != nil {
		q = q.Consistency(*s.consistency)
	}
	if s.pageSize > 0 {
		q = q.PageSize(s.pageSize)
	}
	if len(s.pageState) > 0 {
		q = q.PageState(s.pageState)
	}
	return q
}

// Iter returns a gocql.Iter for manual row consumption.
func (s *SelectBuilder) Iter(ctx context.Context) *gocql.Iter {
	return s.query(ctx).Iter()
}

// All scans every row into dest, which must be a pointer to a slice of
// struct (or pointer to struct).
func (s *SelectBuilder) All(ctx context.Context, dest interface{}) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("cassandra: All requires non-nil pointer, got %T", dest)
	}
	slice := dv.Elem()
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("cassandra: All requires pointer to slice, got %T", dest)
	}
	elemType := slice.Type().Elem()
	isPtr := elemType.Kind() == reflect.Ptr
	structType := elemType
	if isPtr {
		structType = elemType.Elem()
	}
	info, err := ParseModel(reflect.New(structType).Interface())
	if err != nil {
		return err
	}
	iter := s.Iter(ctx)
	columns := iter.Columns()
	defer iter.Close()

	for {
		ptrs := make([]interface{}, len(columns))
		values := make([]reflect.Value, len(columns))
		row := reflect.New(structType).Elem()
		for i, col := range columns {
			f, ok := info.FieldByColumn(col.Name)
			if !ok {
				var discard interface{}
				ptrs[i] = &discard
				continue
			}
			fv := row.FieldByIndex(f.Index)
			values[i] = fv
			ptrs[i] = fv.Addr().Interface()
		}
		if !iter.Scan(ptrs...) {
			break
		}
		if isPtr {
			p := reflect.New(structType)
			p.Elem().Set(row)
			slice.Set(reflect.Append(slice, p))
		} else {
			slice.Set(reflect.Append(slice, row))
		}
	}
	return iter.Close()
}

// One scans a single row into dest (pointer to struct).
func (s *SelectBuilder) One(ctx context.Context, dest interface{}) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("cassandra: One requires non-nil pointer, got %T", dest)
	}
	elem := dv.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("cassandra: One requires pointer to struct, got %T", dest)
	}
	info, err := ParseModel(dest)
	if err != nil {
		return err
	}
	s.Limit(1)
	iter := s.Iter(ctx)
	defer iter.Close()
	columns := iter.Columns()
	ptrs := make([]interface{}, len(columns))
	for i, col := range columns {
		f, ok := info.FieldByColumn(col.Name)
		if !ok {
			var discard interface{}
			ptrs[i] = &discard
			continue
		}
		ptrs[i] = elem.FieldByIndex(f.Index).Addr().Interface()
	}
	if !iter.Scan(ptrs...) {
		if err := iter.Close(); err != nil {
			return err
		}
		return gocql.ErrNotFound
	}
	return iter.Close()
}

// Count returns the COUNT(*) result.
func (s *SelectBuilder) Count(ctx context.Context) (int64, error) {
	clone := *s
	clone.columns = []string{"COUNT(*)"}
	clone.orderBy = nil
	clone.ann = nil
	clone.limit = 0
	stmt, args := clone.CQL()
	var n int64
	err := s.db.session.Query(stmt, args...).WithContext(ctx).Scan(&n)
	return n, err
}

// PageStateOut returns the paging state after the last Iter/All call for
// cursor-style pagination. Must be called after fetching a page.
func (s *SelectBuilder) PageStateOut(iter *gocql.Iter) []byte {
	if iter == nil {
		return nil
	}
	return iter.PageState()
}
