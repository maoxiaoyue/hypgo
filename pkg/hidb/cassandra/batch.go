package cassandra

import (
	"context"
	"fmt"

	"github.com/gocql/gocql"
)

// Statement is anything that renders to CQL plus bind args.
// All builders in this package implement it.
type Statement interface {
	CQL() (string, []interface{})
}

// BatchBuilder wraps gocql.Batch with a typed fluent API.
type BatchBuilder struct {
	db          *CassandraDB
	batch       *gocql.Batch
	consistency *gocql.Consistency
	timestamp   int64
}

// NewBatch creates a new batch of the given type.
// Use gocql.LoggedBatch (default), gocql.UnloggedBatch, or gocql.CounterBatch.
func (c *CassandraDB) NewBatch(bt gocql.BatchType) *BatchBuilder {
	return &BatchBuilder{db: c, batch: c.session.NewBatch(bt)}
}

// NewLoggedBatch is a convenience for gocql.LoggedBatch.
func (c *CassandraDB) NewLoggedBatch() *BatchBuilder { return c.NewBatch(gocql.LoggedBatch) }

// NewUnloggedBatch is a convenience for gocql.UnloggedBatch.
func (c *CassandraDB) NewUnloggedBatch() *BatchBuilder { return c.NewBatch(gocql.UnloggedBatch) }

// NewCounterBatch is a convenience for gocql.CounterBatch.
func (c *CassandraDB) NewCounterBatch() *BatchBuilder { return c.NewBatch(gocql.CounterBatch) }

// Add appends a Statement to the batch.
func (b *BatchBuilder) Add(stmt Statement) *BatchBuilder {
	s, args := stmt.CQL()
	b.batch.Query(s, args...)
	return b
}

// AddRaw appends a raw CQL statement with bind args.
func (b *BatchBuilder) AddRaw(stmt string, args ...interface{}) *BatchBuilder {
	b.batch.Query(stmt, args...)
	return b
}

// Consistency overrides consistency for the batch.
func (b *BatchBuilder) Consistency(cl gocql.Consistency) *BatchBuilder {
	b.consistency = &cl
	return b
}

// Timestamp sets USING TIMESTAMP for the whole batch.
func (b *BatchBuilder) Timestamp(us int64) *BatchBuilder { b.timestamp = us; return b }

// Size returns the number of statements queued.
func (b *BatchBuilder) Size() int { return b.batch.Size() }

// Exec executes the batch.
func (b *BatchBuilder) Exec(ctx context.Context) error {
	if b.consistency != nil {
		b.batch.SetConsistency(*b.consistency)
	}
	if b.timestamp > 0 {
		b.batch.WithTimestamp(b.timestamp)
	}
	return b.db.session.ExecuteBatch(b.batch.WithContext(ctx))
}

// ExecCAS executes the batch as a conditional batch (LWT) returning
// applied + existing row values.
func (b *BatchBuilder) ExecCAS(ctx context.Context, dest ...interface{}) (bool, error) {
	if b.batch.Size() == 0 {
		return false, fmt.Errorf("cassandra: empty batch")
	}
	if b.consistency != nil {
		b.batch.SetConsistency(*b.consistency)
	}
	if b.timestamp > 0 {
		b.batch.WithTimestamp(b.timestamp)
	}
	applied, _, err := b.db.session.ExecuteBatchCAS(b.batch.WithContext(ctx), dest...)
	return applied, err
}
