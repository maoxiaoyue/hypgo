package cassandra

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// CompactionStrategy names as accepted by Cassandra 5.0.
// UnifiedCompactionStrategy (UCS) is new in 5.0 and is the recommended default
// for most workloads.
const (
	CompactionUnified      = "UnifiedCompactionStrategy"
	CompactionSTCS         = "SizeTieredCompactionStrategy"
	CompactionLCS          = "LeveledCompactionStrategy"
	CompactionTWCS         = "TimeWindowCompactionStrategy"
	CompactionIncremental  = "IncrementalCompactionStrategy"
)

// CompressionAlgorithm names supported by Cassandra 5.0 sstable compression.
const (
	CompressionLZ4     = "LZ4Compressor"
	CompressionSnappy  = "SnappyCompressor"
	CompressionDeflate = "DeflateCompressor"
	CompressionZstd    = "ZstdCompressor"
	CompressionNone    = ""
)

// CompactionOptions configures the compaction sub-properties map.
// Extra holds strategy-specific keys (e.g. "sstable_size_in_mb" for LCS,
// "compaction_window_size" for TWCS, "scaling_parameters" for UCS).
type CompactionOptions struct {
	Class string
	Extra map[string]interface{}
}

// ToCQL renders the compaction option map.
func (c CompactionOptions) ToCQL() string {
	class := c.Class
	if class == "" {
		class = CompactionUnified
	}
	var sb strings.Builder
	sb.WriteString("{'class': '")
	sb.WriteString(class)
	sb.WriteString("'")
	keys := make([]string, 0, len(c.Extra))
	for k := range c.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, ", '%s': %s", k, quoteOptionValue(c.Extra[k]))
	}
	sb.WriteString("}")
	return sb.String()
}

// CompressionOptions configures the sstable compression map.
type CompressionOptions struct {
	// SSTableCompression is the class; use CompressionNone or "" to disable.
	SSTableCompression string
	ChunkLengthKB      int     // typically 16 / 32 / 64
	CRCCheckChance     float64 // 0.0 - 1.0
	Extra              map[string]interface{}
}

// ToCQL renders the compression option map.
func (c CompressionOptions) ToCQL() string {
	var sb strings.Builder
	sb.WriteString("{")
	first := true
	write := func(k, v string) {
		if !first {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "'%s': %s", k, v)
		first = false
	}
	if c.SSTableCompression == "" {
		write("sstable_compression", "''")
	} else {
		write("sstable_compression", "'"+c.SSTableCompression+"'")
	}
	if c.ChunkLengthKB > 0 {
		write("chunk_length_in_kb", fmt.Sprintf("%d", c.ChunkLengthKB))
	}
	if c.CRCCheckChance > 0 {
		write("crc_check_chance", fmt.Sprintf("%g", c.CRCCheckChance))
	}
	keys := make([]string, 0, len(c.Extra))
	for k := range c.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		write(k, quoteOptionValue(c.Extra[k]))
	}
	sb.WriteString("}")
	return sb.String()
}

// CachingOptions maps to the CACHING property.
type CachingOptions struct {
	Keys               string // "ALL" | "NONE"
	RowsPerPartition   string // "ALL" | "NONE" | "42" (numeric string allowed)
}

// ToCQL renders the CACHING map.
func (c CachingOptions) ToCQL() string {
	keys := c.Keys
	if keys == "" {
		keys = "ALL"
	}
	rpp := c.RowsPerPartition
	if rpp == "" {
		rpp = "NONE"
	}
	return fmt.Sprintf("{'keys': '%s', 'rows_per_partition': '%s'}", keys, rpp)
}

func quoteOptionValue(v interface{}) string {
	switch x := v.(type) {
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
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("'%s': %s", k, quoteOptionValue(x[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// TableOptions holds all CREATE TABLE WITH ... properties supported by
// Cassandra 5.0. Zero values mean "leave default".
type TableOptions struct {
	Comment                   string
	DefaultTTL                int // seconds
	GCGraceSeconds            int
	BloomFilterFPChance       float64
	MemtableFlushPeriodMs     int
	CRCCheckChance            float64
	ReadRepairChance          float64 // deprecated in 4.0+, kept for compat
	DCLocalReadRepairChance   float64 // deprecated in 4.0+, kept for compat
	MinIndexInterval          int
	MaxIndexInterval          int
	SpeculativeRetry          string // e.g. "99p", "ALWAYS", "NONE"
	AdditionalWritePolicy     string
	CDC                       *bool
	Caching                   *CachingOptions
	Compaction                *CompactionOptions
	Compression               *CompressionOptions
	Extensions                map[string]interface{}
	// Extra accepts any additional option key=value pair the user wants to set,
	// useful for properties added in future Cassandra versions.
	Extra map[string]interface{}
}

// TableBuilder builds CREATE/ALTER/DROP TABLE statements.
type TableBuilder struct {
	db         *CassandraDB
	keyspace   string
	name       string
	columns    []Column
	primaryKey PrimaryKey
	orderBy    []Column // clustering order specs
	options    TableOptions
	ifNotExist bool
}

// Table returns a builder for the named table in the session keyspace.
// Pass "ks.table" to target a specific keyspace.
func (c *CassandraDB) Table(name string) *TableBuilder {
	ks, tbl := splitQualified(name)
	return &TableBuilder{db: c, keyspace: ks, name: tbl, ifNotExist: true}
}

func splitQualified(name string) (string, string) {
	if i := strings.Index(name, "."); i >= 0 {
		return name[:i], name[i+1:]
	}
	return "", name
}

func (t *TableBuilder) qualified() string {
	if t.keyspace != "" {
		return quoteIdent(t.keyspace) + "." + quoteIdent(t.name)
	}
	return quoteIdent(t.name)
}

// Keyspace sets the keyspace for this table.
func (t *TableBuilder) Keyspace(ks string) *TableBuilder {
	t.keyspace = ks
	return t
}

// IfNotExists toggles the IF NOT EXISTS clause (default true).
func (t *TableBuilder) IfNotExists(v bool) *TableBuilder {
	t.ifNotExist = v
	return t
}

// Column adds a regular column.
func (t *TableBuilder) Column(name string, typ DataType) *TableBuilder {
	t.columns = append(t.columns, Column{Name: name, Type: typ, Kind: ColumnRegular})
	return t
}

// Static adds a static column (shared per partition).
func (t *TableBuilder) Static(name string, typ DataType) *TableBuilder {
	t.columns = append(t.columns, Column{Name: name, Type: typ, Kind: ColumnStatic})
	return t
}

// PartitionKey appends partition key column(s).
func (t *TableBuilder) PartitionKey(names ...string) *TableBuilder {
	t.primaryKey.Partition = append(t.primaryKey.Partition, names...)
	return t
}

// ClusteringKey appends clustering key column(s).
func (t *TableBuilder) ClusteringKey(names ...string) *TableBuilder {
	t.primaryKey.Clustering = append(t.primaryKey.Clustering, names...)
	return t
}

// ClusteringOrder adds a clustering ORDER BY entry.
func (t *TableBuilder) ClusteringOrder(column string, order ClusteringOrder) *TableBuilder {
	t.orderBy = append(t.orderBy, Column{Name: column, Order: order})
	return t
}

// Options sets the table options.
func (t *TableBuilder) Options(opts TableOptions) *TableBuilder {
	t.options = opts
	return t
}

// WithComment sets the COMMENT option.
func (t *TableBuilder) WithComment(s string) *TableBuilder {
	t.options.Comment = s
	return t
}

// WithTTL sets default_time_to_live.
func (t *TableBuilder) WithTTL(seconds int) *TableBuilder {
	t.options.DefaultTTL = seconds
	return t
}

// WithCompaction sets the compaction strategy.
func (t *TableBuilder) WithCompaction(opts CompactionOptions) *TableBuilder {
	t.options.Compaction = &opts
	return t
}

// WithCompression sets the compression configuration.
func (t *TableBuilder) WithCompression(opts CompressionOptions) *TableBuilder {
	t.options.Compression = &opts
	return t
}

// WithCaching sets caching options.
func (t *TableBuilder) WithCaching(opts CachingOptions) *TableBuilder {
	t.options.Caching = &opts
	return t
}

// WithCDC toggles change data capture.
func (t *TableBuilder) WithCDC(enabled bool) *TableBuilder {
	t.options.CDC = &enabled
	return t
}

// CreateCQL renders the CREATE TABLE statement.
func (t *TableBuilder) CreateCQL() string {
	var sb strings.Builder
	sb.WriteString("CREATE TABLE ")
	if t.ifNotExist {
		sb.WriteString("IF NOT EXISTS ")
	}
	sb.WriteString(t.qualified())
	sb.WriteString(" (\n")
	columns := t.orderedColumns()
	for i, col := range columns {
		sb.WriteString("  ")
		sb.WriteString(col.Def())
		if i < len(columns)-1 || t.primaryKey.Partition != nil {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	if pk := t.primaryKey.ToCQL(); pk != "" {
		sb.WriteString("  ")
		sb.WriteString(pk)
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	t.renderWithClauses(&sb)
	return sb.String()
}

// orderedColumns returns columns ordered: partition keys, clustering keys,
// static, regular. This produces a stable, readable CREATE TABLE output.
func (t *TableBuilder) orderedColumns() []Column {
	columnByName := make(map[string]Column, len(t.columns))
	for _, c := range t.columns {
		columnByName[c.Name] = c
	}
	// If a column is listed in the PK parts but not added explicitly, keep it
	// as whatever the user declared; we trust user-provided declarations.
	seen := make(map[string]struct{}, len(t.columns))
	ordered := make([]Column, 0, len(t.columns))
	push := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		if c, ok := columnByName[name]; ok {
			ordered = append(ordered, c)
			seen[name] = struct{}{}
		}
	}
	for _, n := range t.primaryKey.Partition {
		push(n)
	}
	for _, n := range t.primaryKey.Clustering {
		push(n)
	}
	for _, c := range t.columns {
		if _, ok := seen[c.Name]; !ok {
			ordered = append(ordered, c)
			seen[c.Name] = struct{}{}
		}
	}
	return ordered
}

func (t *TableBuilder) renderWithClauses(sb *strings.Builder) {
	parts := t.buildWithParts()
	if len(parts) == 0 {
		return
	}
	sb.WriteString(" WITH ")
	sb.WriteString(strings.Join(parts, "\n  AND "))
}

func (t *TableBuilder) buildWithParts() []string {
	var parts []string
	if len(t.orderBy) > 0 {
		specs := make([]string, 0, len(t.orderBy))
		for _, c := range t.orderBy {
			order := c.Order
			if order == "" {
				order = Asc
			}
			specs = append(specs, fmt.Sprintf("%s %s", quoteIdent(c.Name), order))
		}
		parts = append(parts, "CLUSTERING ORDER BY ("+strings.Join(specs, ", ")+")")
	}
	o := t.options
	if o.Comment != "" {
		parts = append(parts, fmt.Sprintf("comment = %s", cqlLiteral(o.Comment)))
	}
	if o.DefaultTTL > 0 {
		parts = append(parts, fmt.Sprintf("default_time_to_live = %d", o.DefaultTTL))
	}
	if o.GCGraceSeconds > 0 {
		parts = append(parts, fmt.Sprintf("gc_grace_seconds = %d", o.GCGraceSeconds))
	}
	if o.BloomFilterFPChance > 0 {
		parts = append(parts, fmt.Sprintf("bloom_filter_fp_chance = %g", o.BloomFilterFPChance))
	}
	if o.MemtableFlushPeriodMs > 0 {
		parts = append(parts, fmt.Sprintf("memtable_flush_period_in_ms = %d", o.MemtableFlushPeriodMs))
	}
	if o.CRCCheckChance > 0 {
		parts = append(parts, fmt.Sprintf("crc_check_chance = %g", o.CRCCheckChance))
	}
	if o.ReadRepairChance > 0 {
		parts = append(parts, fmt.Sprintf("read_repair_chance = %g", o.ReadRepairChance))
	}
	if o.DCLocalReadRepairChance > 0 {
		parts = append(parts, fmt.Sprintf("dclocal_read_repair_chance = %g", o.DCLocalReadRepairChance))
	}
	if o.MinIndexInterval > 0 {
		parts = append(parts, fmt.Sprintf("min_index_interval = %d", o.MinIndexInterval))
	}
	if o.MaxIndexInterval > 0 {
		parts = append(parts, fmt.Sprintf("max_index_interval = %d", o.MaxIndexInterval))
	}
	if o.SpeculativeRetry != "" {
		parts = append(parts, fmt.Sprintf("speculative_retry = %s", cqlLiteral(o.SpeculativeRetry)))
	}
	if o.AdditionalWritePolicy != "" {
		parts = append(parts, fmt.Sprintf("additional_write_policy = %s", cqlLiteral(o.AdditionalWritePolicy)))
	}
	if o.CDC != nil {
		parts = append(parts, fmt.Sprintf("cdc = %t", *o.CDC))
	}
	if o.Caching != nil {
		parts = append(parts, fmt.Sprintf("caching = %s", o.Caching.ToCQL()))
	}
	if o.Compaction != nil {
		parts = append(parts, fmt.Sprintf("compaction = %s", o.Compaction.ToCQL()))
	}
	if o.Compression != nil {
		parts = append(parts, fmt.Sprintf("compression = %s", o.Compression.ToCQL()))
	}
	if len(o.Extensions) > 0 {
		parts = append(parts, fmt.Sprintf("extensions = %s", quoteOptionValue(map[string]interface{}(o.Extensions))))
	}
	if len(o.Extra) > 0 {
		keys := make([]string, 0, len(o.Extra))
		for k := range o.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s = %s", k, quoteOptionValue(o.Extra[k])))
		}
	}
	return parts
}

// DropCQL renders DROP TABLE IF EXISTS ...
func (t *TableBuilder) DropCQL(ifExists bool) string {
	if ifExists {
		return fmt.Sprintf("DROP TABLE IF EXISTS %s", t.qualified())
	}
	return fmt.Sprintf("DROP TABLE %s", t.qualified())
}

// TruncateCQL renders TRUNCATE <table>.
func (t *TableBuilder) TruncateCQL() string {
	return fmt.Sprintf("TRUNCATE %s", t.qualified())
}

// Create executes the CREATE TABLE statement.
func (t *TableBuilder) Create(ctx context.Context) error {
	return t.db.Exec(ctx, t.CreateCQL())
}

// Drop executes DROP TABLE IF EXISTS.
func (t *TableBuilder) Drop(ctx context.Context) error {
	return t.db.Exec(ctx, t.DropCQL(true))
}

// Truncate executes TRUNCATE <table>.
func (t *TableBuilder) Truncate(ctx context.Context) error {
	return t.db.Exec(ctx, t.TruncateCQL())
}

// ===== ALTER TABLE =====

// AlterTableBuilder builds ALTER TABLE statements.
type AlterTableBuilder struct {
	db       *CassandraDB
	keyspace string
	name     string
	actions  []string
}

// AlterTable targets a table for mutation.
func (c *CassandraDB) AlterTable(name string) *AlterTableBuilder {
	ks, tbl := splitQualified(name)
	return &AlterTableBuilder{db: c, keyspace: ks, name: tbl}
}

func (a *AlterTableBuilder) qualified() string {
	if a.keyspace != "" {
		return quoteIdent(a.keyspace) + "." + quoteIdent(a.name)
	}
	return quoteIdent(a.name)
}

// AddColumn appends an ADD clause.
func (a *AlterTableBuilder) AddColumn(name string, typ DataType) *AlterTableBuilder {
	a.actions = append(a.actions, fmt.Sprintf("ADD %s %s", quoteIdent(name), typ))
	return a
}

// DropColumn appends a DROP clause.
func (a *AlterTableBuilder) DropColumn(name string) *AlterTableBuilder {
	a.actions = append(a.actions, fmt.Sprintf("DROP %s", quoteIdent(name)))
	return a
}

// RenameColumn appends a RENAME clause.
func (a *AlterTableBuilder) RenameColumn(oldName, newName string) *AlterTableBuilder {
	a.actions = append(a.actions, fmt.Sprintf("RENAME %s TO %s", quoteIdent(oldName), quoteIdent(newName)))
	return a
}

// WithOptions appends a WITH ... AND ... clause from a TableOptions.
func (a *AlterTableBuilder) WithOptions(opts TableOptions) *AlterTableBuilder {
	tb := &TableBuilder{options: opts}
	parts := tb.buildWithParts()
	if len(parts) > 0 {
		a.actions = append(a.actions, "WITH "+strings.Join(parts, " AND "))
	}
	return a
}

// CQL renders the ALTER TABLE statement. Each action becomes its own statement
// joined by semicolons so callers can exec as a script.
func (a *AlterTableBuilder) CQL() string {
	if len(a.actions) == 0 {
		return ""
	}
	stmts := make([]string, len(a.actions))
	for i, act := range a.actions {
		stmts[i] = fmt.Sprintf("ALTER TABLE %s %s", a.qualified(), act)
	}
	return strings.Join(stmts, ";\n") + ";"
}

// Exec executes the ALTER TABLE statements one at a time.
func (a *AlterTableBuilder) Exec(ctx context.Context) error {
	for _, act := range a.actions {
		stmt := fmt.Sprintf("ALTER TABLE %s %s", a.qualified(), act)
		if err := a.db.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}
