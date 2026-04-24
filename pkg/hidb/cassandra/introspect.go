package cassandra

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Schema is a full snapshot of a Cassandra cluster's user-defined schema
// (excluding system_* keyspaces by default). It is designed to be:
//   - serialisable for AI context / migration diff,
//   - ordered deterministically for stable diffs,
//   - self-contained (one query pass per system_schema table).
type Schema struct {
	CapturedAt time.Time         `json:"captured_at"`
	Keyspaces  []KeyspaceSchema  `json:"keyspaces"`
}

// KeyspaceSchema is the full metadata of one keyspace.
type KeyspaceSchema struct {
	Name          string            `json:"name"`
	Replication   map[string]string `json:"replication"`
	DurableWrites bool              `json:"durable_writes"`
	Tables        []TableSchema     `json:"tables,omitempty"`
	Types         []TypeSchema      `json:"types,omitempty"`
	Views         []ViewSchema      `json:"views,omitempty"`
	Indexes       []IndexSchema     `json:"indexes,omitempty"`
	Functions     []FunctionSchema  `json:"functions,omitempty"`
	Aggregates    []AggregateSchema `json:"aggregates,omitempty"`
	Triggers      []TriggerSchema   `json:"triggers,omitempty"`
}

// TableSchema is the full metadata of one table.
type TableSchema struct {
	Keyspace string            `json:"keyspace"`
	Name     string            `json:"name"`
	Comment  string            `json:"comment,omitempty"`
	Flags    []string          `json:"flags,omitempty"`
	ID       string            `json:"id,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
	Columns  []ColumnSchema    `json:"columns"`
}

// ColumnSchema is one column of a table or view. (Distinct from ColumnInfo
// used by describe.go — this one is tagged for JSON export and keeps more detail.)
type ColumnSchema struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"` // partition_key / clustering / regular / static
	Position        int    `json:"position"`
	Type            string `json:"type"`
	ClusteringOrder string `json:"clustering_order,omitempty"`
}

// TypeSchema is one user-defined type.
type TypeSchema struct {
	Keyspace   string   `json:"keyspace"`
	Name       string   `json:"name"`
	FieldNames []string `json:"field_names"`
	FieldTypes []string `json:"field_types"`
}

// IndexSchema is one secondary or SAI index.
type IndexSchema struct {
	Keyspace string            `json:"keyspace"`
	Table    string            `json:"table"`
	Name     string            `json:"name"`
	Kind     string            `json:"kind"` // COMPOSITES / CUSTOM / KEYS
	Options  map[string]string `json:"options,omitempty"`
}

// ViewSchema is one materialized view.
type ViewSchema struct {
	Keyspace        string         `json:"keyspace"`
	Name            string         `json:"name"`
	BaseTable       string         `json:"base_table"`
	IncludeAll      bool           `json:"include_all"`
	WhereClause     string         `json:"where_clause,omitempty"`
	Columns         []ColumnSchema `json:"columns,omitempty"`
}

// FunctionSchema is one user-defined function.
type FunctionSchema struct {
	Keyspace         string   `json:"keyspace"`
	Name             string   `json:"name"`
	ArgumentNames    []string `json:"argument_names"`
	ArgumentTypes    []string `json:"argument_types"`
	ReturnType       string   `json:"return_type"`
	Language         string   `json:"language"`
	Body             string   `json:"body"`
	CalledOnNullInput bool    `json:"called_on_null_input"`
}

// AggregateSchema is one user-defined aggregate.
type AggregateSchema struct {
	Keyspace      string   `json:"keyspace"`
	Name          string   `json:"name"`
	ArgumentTypes []string `json:"argument_types"`
	StateFunc     string   `json:"state_func"`
	StateType     string   `json:"state_type"`
	FinalFunc     string   `json:"final_func,omitempty"`
	InitCond      string   `json:"init_cond,omitempty"`
	ReturnType    string   `json:"return_type,omitempty"`
}

// TriggerSchema is one registered trigger.
type TriggerSchema struct {
	Keyspace string            `json:"keyspace"`
	Table    string            `json:"table"`
	Name     string            `json:"name"`
	Options  map[string]string `json:"options,omitempty"`
}

// systemKeyspaces is the set of built-in keyspaces that Introspect skips by default.
var systemKeyspaces = map[string]bool{
	"system":               true,
	"system_schema":        true,
	"system_auth":          true,
	"system_distributed":   true,
	"system_traces":        true,
	"system_virtual_schema": true,
	"system_views":         true,
}

// IntrospectOptions controls what Introspect scans.
type IntrospectOptions struct {
	// IncludeSystem captures system_* keyspaces too (default false).
	IncludeSystem bool
	// OnlyKeyspace limits the snapshot to a single keyspace when non-empty.
	OnlyKeyspace string
}

// Introspect reads system_schema.* and returns a full Schema snapshot.
// Useful for migration diff, AI context export, or documentation generation.
func (c *CassandraDB) Introspect(ctx context.Context, opts IntrospectOptions) (*Schema, error) {
	if c.session == nil {
		return nil, fmt.Errorf("cassandra: session not connected")
	}

	keyspaces, err := c.introspectKeyspaces(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("keyspaces: %w", err)
	}
	tables, err := c.introspectTables(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("tables: %w", err)
	}
	columns, err := c.introspectColumns(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("columns: %w", err)
	}
	types, err := c.introspectTypes(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("types: %w", err)
	}
	views, err := c.introspectViews(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("views: %w", err)
	}
	indexes, err := c.introspectIndexes(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("indexes: %w", err)
	}
	funcs, err := c.introspectFunctions(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("functions: %w", err)
	}
	aggs, err := c.introspectAggregates(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("aggregates: %w", err)
	}
	triggers, err := c.introspectTriggers(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("triggers: %w", err)
	}

	// Stitch everything under its keyspace.
	byKS := make(map[string]*KeyspaceSchema, len(keyspaces))
	for i := range keyspaces {
		byKS[keyspaces[i].Name] = &keyspaces[i]
	}

	// Group columns by (ks, table) for table + view stitching.
	colsByTable := make(map[string][]ColumnSchema)
	for _, col := range columns {
		key := col.ksTable
		colsByTable[key] = append(colsByTable[key], col.col)
	}
	// stable order: kind precedence (pk < ck < static < regular), then position.
	for _, list := range colsByTable {
		sortColumnsSchema(list)
	}

	for _, t := range tables {
		ks, ok := byKS[t.Keyspace]
		if !ok {
			continue
		}
		t.Columns = colsByTable[t.Keyspace+"."+t.Name]
		ks.Tables = append(ks.Tables, t)
	}
	for _, v := range views {
		ks, ok := byKS[v.Keyspace]
		if !ok {
			continue
		}
		v.Columns = colsByTable[v.Keyspace+"."+v.Name]
		ks.Views = append(ks.Views, v)
	}
	for _, ut := range types {
		if ks, ok := byKS[ut.Keyspace]; ok {
			ks.Types = append(ks.Types, ut)
		}
	}
	for _, ix := range indexes {
		if ks, ok := byKS[ix.Keyspace]; ok {
			ks.Indexes = append(ks.Indexes, ix)
		}
	}
	for _, f := range funcs {
		if ks, ok := byKS[f.Keyspace]; ok {
			ks.Functions = append(ks.Functions, f)
		}
	}
	for _, a := range aggs {
		if ks, ok := byKS[a.Keyspace]; ok {
			ks.Aggregates = append(ks.Aggregates, a)
		}
	}
	for _, tr := range triggers {
		if ks, ok := byKS[tr.Keyspace]; ok {
			ks.Triggers = append(ks.Triggers, tr)
		}
	}

	// Deterministic ordering for diff stability.
	out := make([]KeyspaceSchema, 0, len(keyspaces))
	for _, k := range keyspaces {
		stitched := byKS[k.Name]
		sortTables(stitched.Tables)
		sortViews(stitched.Views)
		sortByName(stitched.Types, func(t TypeSchema) string { return t.Name })
		sortByName(stitched.Indexes, func(i IndexSchema) string { return i.Table + "." + i.Name })
		sortByName(stitched.Functions, func(f FunctionSchema) string { return f.Name })
		sortByName(stitched.Aggregates, func(a AggregateSchema) string { return a.Name })
		sortByName(stitched.Triggers, func(t TriggerSchema) string { return t.Table + "." + t.Name })
		out = append(out, *stitched)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return &Schema{CapturedAt: time.Now().UTC(), Keyspaces: out}, nil
}

// IntrospectKeyspace is a shortcut for Introspect with OnlyKeyspace set.
func (c *CassandraDB) IntrospectKeyspace(ctx context.Context, keyspace string) (*KeyspaceSchema, error) {
	s, err := c.Introspect(ctx, IntrospectOptions{OnlyKeyspace: keyspace, IncludeSystem: true})
	if err != nil {
		return nil, err
	}
	for i := range s.Keyspaces {
		if s.Keyspaces[i].Name == keyspace {
			return &s.Keyspaces[i], nil
		}
	}
	return nil, fmt.Errorf("cassandra: keyspace %q not found", keyspace)
}

// MarshalJSON serialises the schema. Stable ordering makes this diff-friendly.
func (s *Schema) MarshalJSON() ([]byte, error) {
	type alias Schema
	return json.MarshalIndent((*alias)(s), "", "  ")
}

// ===== row fetchers =====

// keyspaces
func (c *CassandraDB) introspectKeyspaces(ctx context.Context, opts IntrospectOptions) ([]KeyspaceSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, replication, durable_writes FROM system_schema.keyspaces`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []KeyspaceSchema
	var name string
	var repl map[string]string
	var durable bool
	for iter.Scan(&name, &repl, &durable) {
		if !opts.IncludeSystem && systemKeyspaces[name] {
			continue
		}
		if opts.OnlyKeyspace != "" && name != opts.OnlyKeyspace {
			continue
		}
		out = append(out, KeyspaceSchema{Name: name, Replication: repl, DurableWrites: durable})
	}
	return out, iter.Close()
}

// tables
func (c *CassandraDB) introspectTables(ctx context.Context, opts IntrospectOptions) ([]TableSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, comment, flags, id FROM system_schema.tables`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []TableSchema
	var ks, tn, comment, id string
	var flags []string
	for iter.Scan(&ks, &tn, &comment, &flags, &id) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, TableSchema{
			Keyspace: ks, Name: tn, Comment: comment,
			Flags: append([]string(nil), flags...), ID: id,
		})
	}
	return out, iter.Close()
}

// columns (grouped later)
type columnRow struct {
	ksTable string // "ks.table"
	col     ColumnSchema
}

func (c *CassandraDB) introspectColumns(ctx context.Context, opts IntrospectOptions) ([]columnRow, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, column_name, kind, position, type, clustering_order
		 FROM system_schema.columns`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []columnRow
	var ks, tn, cn, kind, typ, order string
	var pos int
	for iter.Scan(&ks, &tn, &cn, &kind, &pos, &typ, &order) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, columnRow{
			ksTable: ks + "." + tn,
			col: ColumnSchema{
				Name: cn, Kind: kind, Position: pos,
				Type: typ, ClusteringOrder: order,
			},
		})
	}
	return out, iter.Close()
}

// types
func (c *CassandraDB) introspectTypes(ctx context.Context, opts IntrospectOptions) ([]TypeSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, type_name, field_names, field_types FROM system_schema.types`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []TypeSchema
	var ks, name string
	var fnames, ftypes []string
	for iter.Scan(&ks, &name, &fnames, &ftypes) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, TypeSchema{
			Keyspace: ks, Name: name,
			FieldNames: append([]string(nil), fnames...),
			FieldTypes: append([]string(nil), ftypes...),
		})
	}
	return out, iter.Close()
}

// views
func (c *CassandraDB) introspectViews(ctx context.Context, opts IntrospectOptions) ([]ViewSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, view_name, base_table_name, include_all_columns, where_clause
		 FROM system_schema.views`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []ViewSchema
	var ks, name, base, where string
	var includeAll bool
	for iter.Scan(&ks, &name, &base, &includeAll, &where) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, ViewSchema{
			Keyspace: ks, Name: name, BaseTable: base,
			IncludeAll: includeAll, WhereClause: where,
		})
	}
	return out, iter.Close()
}

// indexes
func (c *CassandraDB) introspectIndexes(ctx context.Context, opts IntrospectOptions) ([]IndexSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, index_name, kind, options FROM system_schema.indexes`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []IndexSchema
	var ks, tn, name, kind string
	var options map[string]string
	for iter.Scan(&ks, &tn, &name, &kind, &options) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, IndexSchema{
			Keyspace: ks, Table: tn, Name: name, Kind: kind,
			Options: copyStringMap(options),
		})
	}
	return out, iter.Close()
}

// functions
func (c *CassandraDB) introspectFunctions(ctx context.Context, opts IntrospectOptions) ([]FunctionSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, function_name, argument_names, argument_types, return_type,
		        language, body, called_on_null_input FROM system_schema.functions`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []FunctionSchema
	var ks, name, rt, lang, body string
	var argn, argt []string
	var calledOnNull bool
	for iter.Scan(&ks, &name, &argn, &argt, &rt, &lang, &body, &calledOnNull) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, FunctionSchema{
			Keyspace: ks, Name: name,
			ArgumentNames: append([]string(nil), argn...),
			ArgumentTypes: append([]string(nil), argt...),
			ReturnType:    rt, Language: lang, Body: body,
			CalledOnNullInput: calledOnNull,
		})
	}
	return out, iter.Close()
}

// aggregates
func (c *CassandraDB) introspectAggregates(ctx context.Context, opts IntrospectOptions) ([]AggregateSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, aggregate_name, argument_types, state_func, state_type,
		        final_func, initcond, return_type FROM system_schema.aggregates`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []AggregateSchema
	var ks, name, sfunc, stype, ffunc, initcond, rt string
	var argt []string
	for iter.Scan(&ks, &name, &argt, &sfunc, &stype, &ffunc, &initcond, &rt) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, AggregateSchema{
			Keyspace: ks, Name: name,
			ArgumentTypes: append([]string(nil), argt...),
			StateFunc:     sfunc, StateType: stype,
			FinalFunc: ffunc, InitCond: initcond, ReturnType: rt,
		})
	}
	return out, iter.Close()
}

// triggers
func (c *CassandraDB) introspectTriggers(ctx context.Context, opts IntrospectOptions) ([]TriggerSchema, error) {
	iter := c.session.Query(
		`SELECT keyspace_name, table_name, trigger_name, options FROM system_schema.triggers`,
	).WithContext(ctx).Iter()
	defer iter.Close()

	var out []TriggerSchema
	var ks, tn, name string
	var options map[string]string
	for iter.Scan(&ks, &tn, &name, &options) {
		if !opts.IncludeSystem && systemKeyspaces[ks] {
			continue
		}
		if opts.OnlyKeyspace != "" && ks != opts.OnlyKeyspace {
			continue
		}
		out = append(out, TriggerSchema{
			Keyspace: ks, Table: tn, Name: name,
			Options: copyStringMap(options),
		})
	}
	return out, iter.Close()
}

// ===== sorting helpers =====

func sortColumnsSchema(xs []ColumnSchema) {
	kindOrder := map[string]int{
		"partition_key": 0,
		"clustering":    1,
		"static":        2,
		"regular":       3,
	}
	sort.SliceStable(xs, func(i, j int) bool {
		ki, kj := kindOrder[xs[i].Kind], kindOrder[xs[j].Kind]
		if ki != kj {
			return ki < kj
		}
		if xs[i].Position != xs[j].Position {
			return xs[i].Position < xs[j].Position
		}
		return xs[i].Name < xs[j].Name
	})
}

func sortTables(xs []TableSchema) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}

func sortViews(xs []ViewSchema) {
	sort.Slice(xs, func(i, j int) bool { return xs[i].Name < xs[j].Name })
}

func sortByName[T any](xs []T, key func(T) string) {
	sort.Slice(xs, func(i, j int) bool { return key(xs[i]) < key(xs[j]) })
}

func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ===== Diff =====

// SchemaChangeKind classifies the kind of change.
type SchemaChangeKind string

const (
	ChangeAddKeyspace  SchemaChangeKind = "add_keyspace"
	ChangeDropKeyspace SchemaChangeKind = "drop_keyspace"
	ChangeAlterKeyspace SchemaChangeKind = "alter_keyspace"
	ChangeAddTable     SchemaChangeKind = "add_table"
	ChangeDropTable    SchemaChangeKind = "drop_table"
	ChangeAlterTable   SchemaChangeKind = "alter_table"
	ChangeAddColumn    SchemaChangeKind = "add_column"
	ChangeDropColumn   SchemaChangeKind = "drop_column"
	ChangeAlterColumn  SchemaChangeKind = "alter_column"
)

// SchemaChange is one delta produced by SchemaDiff.
type SchemaChange struct {
	Kind     SchemaChangeKind `json:"kind"`
	Keyspace string           `json:"keyspace,omitempty"`
	Table    string           `json:"table,omitempty"`
	Column   string           `json:"column,omitempty"`
	Detail   string           `json:"detail,omitempty"`
}

// SchemaDiff returns the list of changes that would transform `old` into `new`.
// Suitable for migration preview or AI-assisted schema review.
func SchemaDiff(old, new *Schema) []SchemaChange {
	var out []SchemaChange
	oldKS := indexKeyspaces(old)
	newKS := indexKeyspaces(new)

	// dropped / altered keyspaces
	for name, o := range oldKS {
		n, exists := newKS[name]
		if !exists {
			out = append(out, SchemaChange{Kind: ChangeDropKeyspace, Keyspace: name})
			continue
		}
		if !replicationEqual(o.Replication, n.Replication) || o.DurableWrites != n.DurableWrites {
			out = append(out, SchemaChange{
				Kind: ChangeAlterKeyspace, Keyspace: name,
				Detail: fmt.Sprintf("replication/durable_writes changed"),
			})
		}
		out = append(out, tableDiff(o, n)...)
	}
	// added keyspaces
	for name, n := range newKS {
		if _, exists := oldKS[name]; !exists {
			out = append(out, SchemaChange{Kind: ChangeAddKeyspace, Keyspace: name})
			for _, t := range n.Tables {
				out = append(out, SchemaChange{Kind: ChangeAddTable, Keyspace: name, Table: t.Name})
			}
		}
	}
	return out
}

func tableDiff(old, new *KeyspaceSchema) []SchemaChange {
	var out []SchemaChange
	oldT := make(map[string]TableSchema, len(old.Tables))
	for _, t := range old.Tables {
		oldT[t.Name] = t
	}
	newT := make(map[string]TableSchema, len(new.Tables))
	for _, t := range new.Tables {
		newT[t.Name] = t
	}
	for name, o := range oldT {
		n, exists := newT[name]
		if !exists {
			out = append(out, SchemaChange{Kind: ChangeDropTable, Keyspace: old.Name, Table: name})
			continue
		}
		out = append(out, columnDiff(old.Name, name, o.Columns, n.Columns)...)
	}
	for name := range newT {
		if _, exists := oldT[name]; !exists {
			out = append(out, SchemaChange{Kind: ChangeAddTable, Keyspace: new.Name, Table: name})
		}
	}
	return out
}

func columnDiff(ks, table string, old, new []ColumnSchema) []SchemaChange {
	var out []SchemaChange
	oldC := make(map[string]ColumnSchema, len(old))
	for _, c := range old {
		oldC[c.Name] = c
	}
	newC := make(map[string]ColumnSchema, len(new))
	for _, c := range new {
		newC[c.Name] = c
	}
	for name, o := range oldC {
		n, exists := newC[name]
		if !exists {
			out = append(out, SchemaChange{Kind: ChangeDropColumn, Keyspace: ks, Table: table, Column: name})
			continue
		}
		if o.Type != n.Type || o.Kind != n.Kind || o.ClusteringOrder != n.ClusteringOrder {
			out = append(out, SchemaChange{
				Kind: ChangeAlterColumn, Keyspace: ks, Table: table, Column: name,
				Detail: fmt.Sprintf("%s/%s → %s/%s", o.Kind, o.Type, n.Kind, n.Type),
			})
		}
	}
	for name := range newC {
		if _, exists := oldC[name]; !exists {
			out = append(out, SchemaChange{Kind: ChangeAddColumn, Keyspace: ks, Table: table, Column: name})
		}
	}
	return out
}

func indexKeyspaces(s *Schema) map[string]*KeyspaceSchema {
	out := make(map[string]*KeyspaceSchema)
	if s == nil {
		return out
	}
	for i := range s.Keyspaces {
		out[s.Keyspaces[i].Name] = &s.Keyspaces[i]
	}
	return out
}

func replicationEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
