package cassandra

import (
	"context"
	"fmt"
)

// TableFromModel reflects a Go struct and returns a preconfigured TableBuilder.
func (c *CassandraDB) TableFromModel(model interface{}, opts ...TableOption) (*TableBuilder, error) {
	info, err := ParseModel(model)
	if err != nil {
		return nil, err
	}
	name := info.Table
	if info.Keyspace != "" {
		name = info.Keyspace + "." + info.Table
	}
	t := c.Table(name)
	for _, f := range info.Fields {
		switch f.Kind {
		case ColumnStatic:
			t.Static(f.Name, f.Type)
		default:
			t.Column(f.Name, f.Type)
		}
	}
	t.PartitionKey(info.PartitionKey...)
	t.ClusteringKey(info.Clustering...)
	for _, ob := range info.OrderBy {
		t.ClusteringOrder(ob.Name, ob.Order)
	}
	for _, opt := range opts {
		opt(&t.options)
	}
	return t, nil
}

// AutoMigrate creates the tables for every model (idempotent when
// IF NOT EXISTS is set). Models should be pointers to structs.
func (c *CassandraDB) AutoMigrate(ctx context.Context, models ...interface{}) error {
	if c.session == nil {
		return fmt.Errorf("cassandra: session not connected")
	}
	for _, m := range models {
		t, err := c.TableFromModel(m)
		if err != nil {
			return err
		}
		if err := t.Create(ctx); err != nil {
			return fmt.Errorf("cassandra: create table %s failed: %w", t.name, err)
		}
	}
	return nil
}

// TableOption customises the generated TableOptions when using TableFromModel.
type TableOption func(*TableOptions)

// WithTableComment sets COMMENT.
func WithTableComment(s string) TableOption {
	return func(o *TableOptions) { o.Comment = s }
}

// WithDefaultTTL sets default_time_to_live.
func WithDefaultTTL(sec int) TableOption {
	return func(o *TableOptions) { o.DefaultTTL = sec }
}

// WithCompactionOption sets the compaction strategy.
func WithCompactionOption(c CompactionOptions) TableOption {
	return func(o *TableOptions) { o.Compaction = &c }
}

// WithCompressionOption sets the compression strategy.
func WithCompressionOption(c CompressionOptions) TableOption {
	return func(o *TableOptions) { o.Compression = &c }
}

// WithCachingOption sets caching.
func WithCachingOption(c CachingOptions) TableOption {
	return func(o *TableOptions) { o.Caching = &c }
}

// WithCDC toggles change data capture.
func WithCDC(v bool) TableOption {
	return func(o *TableOptions) { o.CDC = &v }
}
