package cassandra

// Defaults applied to keyspace and table builders when callers do not
// specify values. Values aim to align with Cassandra 5.0 server defaults
// so emitting them changes syntax only (not semantics), making generated
// CQL reproducible and diff-friendly.
//
// Each default is a package-level variable so callers can override them
// globally (e.g. in init()) before building schema DDL.
var (
	// DefaultKeyspaceReplication is used by KeyspaceBuilder.CreateCQL when
	// the caller does not call Simple / NetworkTopology / Replication.
	DefaultKeyspaceReplication = NewSimpleReplication(1)

	// DefaultDurableWrites is emitted when DurableWrites is not set.
	// Cassandra's own default is true; we emit it explicitly for clarity.
	DefaultDurableWrites = true

	// DefaultCompaction — UnifiedCompactionStrategy is the Cassandra 5.0
	// recommended general-purpose strategy.
	DefaultCompaction = CompactionOptions{Class: CompactionUnified}

	// DefaultCompression — LZ4 with 16 KB chunks matches Cassandra defaults.
	DefaultCompression = CompressionOptions{
		SSTableCompression: CompressionLZ4,
		ChunkLengthKB:      16,
	}

	// DefaultCaching — keys cached, rows not cached.
	DefaultCaching = CachingOptions{Keys: "ALL", RowsPerPartition: "NONE"}

	// DefaultGCGraceSeconds — 10 days (Cassandra default).
	DefaultGCGraceSeconds = 864000

	// DefaultBloomFilterFPChance — 0.01 matches Cassandra default.
	DefaultBloomFilterFPChance = 0.01

	// DefaultSpeculativeRetry — "99p" (99th percentile) matches Cassandra default.
	DefaultSpeculativeRetry = "99p"
)

// applyKeyspaceDefaults fills zero-valued fields on a KeyspaceOptions.
func applyKeyspaceDefaults(o *KeyspaceOptions) {
	if o.Replication.Class == "" && o.Replication.Factor == 0 && len(o.Replication.DataCenters) == 0 {
		o.Replication = DefaultKeyspaceReplication
	}
	if o.DurableWrites == nil {
		v := DefaultDurableWrites
		o.DurableWrites = &v
	}
}

// applyTableDefaults fills zero-valued fields on a TableOptions.
// Only top-level options with no explicit setting are filled; user-provided
// values are never overwritten.
func applyTableDefaults(o *TableOptions) {
	if o.Compaction == nil {
		c := DefaultCompaction
		o.Compaction = &c
	}
	if o.Compression == nil {
		c := DefaultCompression
		o.Compression = &c
	}
	if o.Caching == nil {
		c := DefaultCaching
		o.Caching = &c
	}
	if o.GCGraceSeconds == 0 {
		o.GCGraceSeconds = DefaultGCGraceSeconds
	}
	if o.BloomFilterFPChance == 0 {
		o.BloomFilterFPChance = DefaultBloomFilterFPChance
	}
	if o.SpeculativeRetry == "" {
		o.SpeculativeRetry = DefaultSpeculativeRetry
	}
}
