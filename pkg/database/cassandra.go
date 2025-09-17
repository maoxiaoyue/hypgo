package database

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gocql/gocql"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// CassandraDB 提供對Cassandra和ScyllaDB的優化支援
type CassandraDB struct {
	config       *config.CassandraConfig
	session      *gocql.Session
	preparedStmt map[string]*gocql.Query
	mu           sync.RWMutex
	logger       *logger.Logger

	// 連線池管理
	poolConfig  *PoolConfig
	retryPolicy gocql.RetryPolicy

	// 效能監控
	metrics *CassandraMetrics
}

// PoolConfig 連線池配置
type PoolConfig struct {
	MaxConns       int
	MaxStreams     int
	NumConns       int
	ConnectTimeout time.Duration
	Timeout        time.Duration
	IdleTimeout    time.Duration
	KeepAlive      time.Duration
}

// CassandraMetrics 效能指標
type CassandraMetrics struct {
	QueryCount      int64
	ErrorCount      int64
	AvgResponseTime time.Duration
	mu              sync.RWMutex
}

// QueryOptions 查詢選項
type QueryOptions struct {
	Consistency       gocql.Consistency
	PageSize          int
	PageState         []byte
	RetryPolicy       gocql.RetryPolicy
	Timeout           time.Duration
	Context           context.Context
	Idempotent        bool
	SerialConsistency gocql.SerialConsistency
	AllowFiltering    bool // 預設為 false，強烈不建議使用
}

// BatchBuilder 批次操作建構器
type BatchBuilder struct {
	db      *CassandraDB
	batch   *gocql.Batch
	entries []batchEntry // 儲存所有的查詢以支援 context
}

// batchEntry 批次項目
type batchEntry struct {
	query string
	args  []interface{}
}

// NewCassandra 建立新的Cassandra/ScyllaDB連線
func NewCassandra(cfg *config.CassandraConfig) (*CassandraDB, error) {
	cluster := gocql.NewCluster(cfg.Hosts...)

	// 基本配置
	cluster.Keyspace = cfg.Keyspace
	cluster.ProtoVersion = 4

	// 效能優化配置
	cluster.NumConns = cfg.NumConns // 每個主機的連線數
	if cluster.NumConns == 0 {
		cluster.NumConns = 3 // 預設3個連線
	}

	// 一致性級別優化
	cluster.Consistency = parseConsistency(cfg.Consistency)

	// 連線池配置
	cluster.ConnectTimeout = time.Duration(cfg.ConnectTimeout) * time.Second
	cluster.Timeout = time.Duration(cfg.Timeout) * time.Second

	// 壓縮配置（提高傳輸效率）
	if cfg.Compression != "" {
		cluster.Compressor = gocql.SnappyCompressor{}
	}

	// 重試策略 - 使用介面類型
	var retryPolicy gocql.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
		NumRetries: cfg.MaxRetries,
		Min:        100 * time.Millisecond,
		Max:        10 * time.Second,
	}
	cluster.RetryPolicy = retryPolicy

	// 連線池大小優化
	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
		gocql.RoundRobinHostPolicy(),
	)

	// 建立認證（如果需要）
	if cfg.Username != "" && cfg.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}

	// 建立session
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create cassandra session: %w", err)
	}

	db := &CassandraDB{
		config:       cfg,
		session:      session,
		preparedStmt: make(map[string]*gocql.Query),
		retryPolicy:  retryPolicy,
		metrics:      &CassandraMetrics{},
		poolConfig: &PoolConfig{
			MaxConns:       cfg.MaxConns,
			NumConns:       cfg.NumConns,
			ConnectTimeout: time.Duration(cfg.ConnectTimeout) * time.Second,
			Timeout:        time.Duration(cfg.Timeout) * time.Second,
		},
	}

	// 初始化logger - 修正類型問題
	if cfg.EnableLogging == "true" || cfg.EnableLogging == "1" {
		db.logger = logger.NewLogger()
	}

	return db, nil
}

// parseConsistency 解析一致性級別
func parseConsistency(level string) gocql.Consistency {
	switch strings.ToUpper(level) {
	case "ANY":
		return gocql.Any
	case "ONE":
		return gocql.One
	case "TWO":
		return gocql.Two
	case "THREE":
		return gocql.Three
	case "QUORUM":
		return gocql.Quorum
	case "ALL":
		return gocql.All
	case "LOCAL_QUORUM":
		return gocql.LocalQuorum
	case "EACH_QUORUM":
		return gocql.EachQuorum
	case "LOCAL_ONE":
		return gocql.LocalOne
	default:
		return gocql.Quorum
	}
}

// validateQuery 驗證查詢，防止使用 ALLOW FILTERING
func (c *CassandraDB) validateQuery(query string) error {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	if strings.Contains(upperQuery, "ALLOW FILTERING") {
		return fmt.Errorf("ALLOW FILTERING is not permitted for performance reasons. Please use proper indexes or redesign your data model")
	}
	return nil
}

// Session 獲取session
func (c *CassandraDB) Session() *gocql.Session {
	return c.session
}

// Query 建立查詢 - 加入 ALLOW FILTERING 檢查
func (c *CassandraDB) Query(query string, args ...interface{}) *gocql.Query {
	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		if c.logger != nil {
			c.logger.Error("Query validation failed", "error", err, "query", query)
		}
		// 返回一個會失敗的查詢
		return c.session.Query("SELECT 1 WHERE 1=0")
	}

	q := c.session.Query(query, args...)
	c.recordMetrics()
	return q
}

// QueryWithOptions 使用選項執行查詢
func (c *CassandraDB) QueryWithOptions(query string, opts *QueryOptions, args ...interface{}) *gocql.Query {
	// 檢查是否嘗試使用 ALLOW FILTERING
	if opts != nil && opts.AllowFiltering {
		if c.logger != nil {
			c.logger.Warn("Attempt to use ALLOW FILTERING was blocked", "query", query)
		}
		// 重置為 false
		opts.AllowFiltering = false
	}

	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		if c.logger != nil {
			c.logger.Error("Query validation failed", "error", err, "query", query)
		}
		return c.session.Query("SELECT 1 WHERE 1=0")
	}

	q := c.session.Query(query, args...)

	if opts != nil {
		if opts.Consistency != 0 {
			q = q.Consistency(opts.Consistency)
		}
		if opts.PageSize > 0 {
			q = q.PageSize(opts.PageSize)
		}
		if opts.PageState != nil {
			q = q.PageState(opts.PageState)
		}
		if opts.RetryPolicy != nil {
			q = q.RetryPolicy(opts.RetryPolicy)
		}
		if opts.Context != nil {
			q = q.WithContext(opts.Context)
		}
		if opts.Idempotent {
			q = q.Idempotent(true)
		}
		if opts.SerialConsistency != 0 {
			q = q.SerialConsistency(opts.SerialConsistency)
		}
	}

	c.recordMetrics()
	return q
}

// Prepare 預處理語句（提高效能）
func (c *CassandraDB) Prepare(query string) error {
	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	q := c.session.Query(query)
	c.preparedStmt[query] = q

	if c.logger != nil {
		c.logger.Debug("Prepared statement", "query", query)
	}

	return nil
}

// ExecutePrepared 執行預處理語句
func (c *CassandraDB) ExecutePrepared(query string, args ...interface{}) error {
	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		return err
	}

	c.mu.RLock()
	q, exists := c.preparedStmt[query]
	c.mu.RUnlock()

	if !exists {
		// 自動準備語句
		if err := c.Prepare(query); err != nil {
			return err
		}
		c.mu.RLock()
		q = c.preparedStmt[query]
		c.mu.RUnlock()
	}

	// 綁定參數並執行
	return q.Bind(args...).Exec()
}

// NewBatch 建立批次操作
func (c *CassandraDB) NewBatch(batchType gocql.BatchType) *BatchBuilder {
	return &BatchBuilder{
		db:      c,
		batch:   c.session.NewBatch(batchType),
		entries: []batchEntry{},
	}
}

// Add 添加語句到批次
func (b *BatchBuilder) Add(query string, args ...interface{}) *BatchBuilder {
	// 驗證查詢
	if err := b.db.validateQuery(query); err != nil {
		if b.db.logger != nil {
			b.db.logger.Error("Batch query validation failed", "error", err, "query", query)
		}
		return b
	}

	// 添加到批次
	b.batch.Query(query, args...)

	// 儲存查詢以支援 context
	b.entries = append(b.entries, batchEntry{
		query: query,
		args:  args,
	})

	return b
}

// Execute 執行批次操作
func (b *BatchBuilder) Execute() error {
	return b.db.session.ExecuteBatch(b.batch)
}

// ExecuteWithContext 使用context執行批次（簡化版本）
func (b *BatchBuilder) ExecuteWithContext(ctx context.Context) error {
	// 方法1：使用 Observer 來處理 context（如果需要超時控制）
	done := make(chan error, 1)

	go func() {
		done <- b.db.session.ExecuteBatch(b.batch)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// ExecuteWithTimeout 使用超時執行批次
func (b *BatchBuilder) ExecuteWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return b.ExecuteWithContext(ctx)
}

// SetConsistency 設定批次一致性級別
func (b *BatchBuilder) SetConsistency(consistency gocql.Consistency) *BatchBuilder {
	b.batch.SetConsistency(consistency)
	return b
}

// SetSerialConsistency 設定序列一致性級別
func (b *BatchBuilder) SetSerialConsistency(consistency gocql.SerialConsistency) *BatchBuilder {
	b.batch.SerialConsistency(consistency)
	return b
}

// Size 獲取批次中的語句數量
func (b *BatchBuilder) Size() int {
	return len(b.entries)
}

// Clear 清空批次
func (b *BatchBuilder) Clear() *BatchBuilder {
	b.batch = b.db.session.NewBatch(b.batch.Type)
	b.entries = []batchEntry{}
	return b
}

// Type 獲取批次類型
func (b *BatchBuilder) Type() gocql.BatchType {
	return b.batch.Type
}

// Close 關閉連線
func (c *CassandraDB) Close() {
	if c.session != nil {
		c.session.Close()
		if c.logger != nil {
			c.logger.Info("Cassandra connection closed")
		}
	}
}

// CreateTable 建立表格
func (c *CassandraDB) CreateTable(tableName string, schema string) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			%s
		)`, tableName, schema)

	if err := c.session.Query(query).Exec(); err != nil {
		c.recordError()
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	if c.logger != nil {
		c.logger.Info("Table created", "table", tableName)
	}

	return nil
}

// Insert 插入資料（優化版）
func (c *CassandraDB) Insert(table string, data map[string]interface{}) error {
	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	values := make([]interface{}, 0, len(data))

	for col, val := range data {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	// 使用預處理語句提高效能
	return c.ExecutePrepared(query, values...)
}

// InsertWithTTL 插入資料並設定TTL
func (c *CassandraDB) InsertWithTTL(table string, data map[string]interface{}, ttl int) error {
	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	values := make([]interface{}, 0, len(data))

	for col, val := range data {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) USING TTL %d",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
		ttl,
	)

	return c.ExecutePrepared(query, values...)
}

// BatchInsert 批次插入（提高效能）
func (c *CassandraDB) BatchInsert(table string, dataList []map[string]interface{}) error {
	if len(dataList) == 0 {
		return nil
	}

	batch := c.NewBatch(gocql.LoggedBatch)

	for _, data := range dataList {
		columns := make([]string, 0, len(data))
		placeholders := make([]string, 0, len(data))
		values := make([]interface{}, 0, len(data))

		for col, val := range data {
			columns = append(columns, col)
			placeholders = append(placeholders, "?")
			values = append(values, val)
		}

		query := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)

		batch.Add(query, values...)
	}

	return batch.Execute()
}

// BatchInsertWithOptions 批次插入（帶選項）
func (c *CassandraDB) BatchInsertWithOptions(table string, dataList []map[string]interface{}, consistency gocql.Consistency, timeout time.Duration) error {
	if len(dataList) == 0 {
		return nil
	}

	batch := c.NewBatch(gocql.LoggedBatch)

	// 設定一致性級別
	if consistency != 0 {
		batch.SetConsistency(consistency)
	}

	for _, data := range dataList {
		columns := make([]string, 0, len(data))
		placeholders := make([]string, 0, len(data))
		values := make([]interface{}, 0, len(data))

		for col, val := range data {
			columns = append(columns, col)
			placeholders = append(placeholders, "?")
			values = append(values, val)
		}

		query := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			table,
			strings.Join(columns, ", "),
			strings.Join(placeholders, ", "),
		)

		batch.Add(query, values...)
	}

	// 使用超時執行
	if timeout > 0 {
		return batch.ExecuteWithTimeout(timeout)
	}

	return batch.Execute()
}

// Select 查詢資料（支援分頁） - 改進版，鼓勵使用索引
func (c *CassandraDB) Select(table string, columns []string, where string, args ...interface{}) (*gocql.Iter, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s",
		strings.Join(columns, ", "),
		table,
	)

	if where != "" {
		query += " WHERE " + where
	}

	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		return nil, err
	}

	return c.session.Query(query, args...).Iter(), nil
}

// SelectOne 查詢單筆資料
func (c *CassandraDB) SelectOne(table string, columns []string, where string, args ...interface{}) (map[string]interface{}, error) {
	iter, err := c.Select(table, columns, where, args...)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	result := make(map[string]interface{})
	if !iter.MapScan(result) {
		return nil, fmt.Errorf("no records found")
	}

	return result, iter.Close()
}

// SelectWithPaging 分頁查詢
func (c *CassandraDB) SelectWithPaging(table string, columns []string, where string, pageSize int, pageState []byte, args ...interface{}) (*gocql.Iter, []byte, error) {
	query := fmt.Sprintf(
		"SELECT %s FROM %s",
		strings.Join(columns, ", "),
		table,
	)

	if where != "" {
		query += " WHERE " + where
	}

	// 驗證查詢
	if err := c.validateQuery(query); err != nil {
		return nil, nil, err
	}

	q := c.session.Query(query, args...).PageSize(pageSize)
	if pageState != nil {
		q = q.PageState(pageState)
	}

	iter := q.Iter()
	return iter, iter.PageState(), nil
}

// Update 更新資料
func (c *CassandraDB) Update(table string, updates map[string]interface{}, where string, args ...interface{}) error {
	setClauses := make([]string, 0, len(updates))
	values := make([]interface{}, 0, len(updates)+len(args))

	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		values = append(values, val)
	}

	values = append(values, args...)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		table,
		strings.Join(setClauses, ", "),
		where,
	)

	return c.ExecutePrepared(query, values...)
}

// UpdateWithTTL 更新資料並設定TTL
func (c *CassandraDB) UpdateWithTTL(table string, updates map[string]interface{}, ttl int, where string, args ...interface{}) error {
	setClauses := make([]string, 0, len(updates))
	values := make([]interface{}, 0, len(updates)+len(args))

	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		values = append(values, val)
	}

	values = append(values, args...)

	query := fmt.Sprintf(
		"UPDATE %s USING TTL %d SET %s WHERE %s",
		table,
		ttl,
		strings.Join(setClauses, ", "),
		where,
	)

	return c.ExecutePrepared(query, values...)
}

// Delete 刪除資料
func (c *CassandraDB) Delete(table string, where string, args ...interface{}) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", table, where)
	return c.ExecutePrepared(query, args...)
}

// Truncate 清空表格
func (c *CassandraDB) Truncate(table string) error {
	query := fmt.Sprintf("TRUNCATE %s", table)
	return c.session.Query(query).Exec()
}

// CreateIndex 建立索引 - 建議使用以避免 ALLOW FILTERING
func (c *CassandraDB) CreateIndex(indexName, table, column string) error {
	query := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (%s)",
		indexName, table, column,
	)

	if err := c.session.Query(query).Exec(); err != nil {
		return fmt.Errorf("failed to create index %s: %w", indexName, err)
	}

	if c.logger != nil {
		c.logger.Info("Index created", "index", indexName, "table", table, "column", column)
	}

	return nil
}

// CreateCustomIndex 建立自訂索引（支援複合索引）
func (c *CassandraDB) CreateCustomIndex(indexName, table string, columns []string, className string) error {
	columnList := strings.Join(columns, ", ")
	query := fmt.Sprintf(
		"CREATE CUSTOM INDEX IF NOT EXISTS %s ON %s (%s) USING '%s'",
		indexName, table, columnList, className,
	)

	if err := c.session.Query(query).Exec(); err != nil {
		return fmt.Errorf("failed to create custom index %s: %w", indexName, err)
	}

	return nil
}

// DropIndex 刪除索引
func (c *CassandraDB) DropIndex(indexName string) error {
	query := fmt.Sprintf("DROP INDEX IF EXISTS %s", indexName)
	return c.session.Query(query).Exec()
}

// ExecuteAsync 非同步執行查詢
func (c *CassandraDB) ExecuteAsync(query string, args ...interface{}) chan error {
	errChan := make(chan error, 1)

	// 先驗證查詢
	if err := c.validateQuery(query); err != nil {
		errChan <- err
		close(errChan)
		return errChan
	}

	go func() {
		err := c.session.Query(query, args...).Exec()
		errChan <- err
		close(errChan)
	}()

	return errChan
}

// Transaction 執行事務（僅ScyllaDB支援）
func (c *CassandraDB) Transaction(queries []string, args [][]interface{}) error {
	if len(queries) != len(args) {
		return fmt.Errorf("queries and args length mismatch")
	}

	// 驗證所有查詢
	for _, query := range queries {
		if err := c.validateQuery(query); err != nil {
			return err
		}
	}

	batch := c.NewBatch(gocql.LoggedBatch)

	for i, query := range queries {
		batch.Add(query, args[i]...)
	}

	return batch.Execute()
}

// GetMetrics 獲取效能指標
func (c *CassandraDB) GetMetrics() *CassandraMetrics {
	c.metrics.mu.RLock()
	defer c.metrics.mu.RUnlock()

	return &CassandraMetrics{
		QueryCount:      c.metrics.QueryCount,
		ErrorCount:      c.metrics.ErrorCount,
		AvgResponseTime: c.metrics.AvgResponseTime,
	}
}

// recordMetrics 記錄指標
func (c *CassandraDB) recordMetrics() {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()
	c.metrics.QueryCount++
}

// recordError 記錄錯誤
func (c *CassandraDB) recordError() {
	c.metrics.mu.Lock()
	defer c.metrics.mu.Unlock()
	c.metrics.ErrorCount++

	if c.logger != nil {
		c.logger.Warn("Cassandra query error recorded", "total_errors", c.metrics.ErrorCount)
	}
}

// HealthCheck 健康檢查
func (c *CassandraDB) HealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := "SELECT now() FROM system.local"
	iter := c.session.Query(query).WithContext(ctx).Iter()
	defer iter.Close()

	var result interface{}
	if !iter.Scan(&result) {
		return fmt.Errorf("health check failed: no result")
	}

	return iter.Close()
}

// InsertIfNotExists 條件插入（LWT）
func (c *CassandraDB) InsertIfNotExists(table string, data map[string]interface{}) (bool, error) {
	columns := make([]string, 0, len(data))
	placeholders := make([]string, 0, len(data))
	values := make([]interface{}, 0, len(data))

	for col, val := range data {
		columns = append(columns, col)
		placeholders = append(placeholders, "?")
		values = append(values, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) IF NOT EXISTS",
		table,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	var applied bool
	if err := c.session.Query(query, values...).Scan(&applied); err != nil {
		return false, err
	}

	return applied, nil
}

// UpdateIfExists 條件更新
func (c *CassandraDB) UpdateIfExists(table string, updates map[string]interface{}, condition string, where string, args ...interface{}) (bool, error) {
	setClauses := make([]string, 0, len(updates))
	values := make([]interface{}, 0, len(updates)+len(args))

	for col, val := range updates {
		setClauses = append(setClauses, col+" = ?")
		values = append(values, val)
	}

	values = append(values, args...)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s IF %s",
		table,
		strings.Join(setClauses, ", "),
		where,
		condition,
	)

	var applied bool
	if err := c.session.Query(query, values...).Scan(&applied); err != nil {
		return false, err
	}

	return applied, nil
}

// IncrementCounter 增加計數器
func (c *CassandraDB) IncrementCounter(table, counterColumn, where string, increment int64, args ...interface{}) error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = %s + ? WHERE %s",
		table, counterColumn, counterColumn, where,
	)

	values := []interface{}{increment}
	values = append(values, args...)

	return c.session.Query(query, values...).Exec()
}

// CreateMaterializedView 建立物化視圖（避免使用 ALLOW FILTERING）
func (c *CassandraDB) CreateMaterializedView(viewName, baseTable string, columns []string, primaryKey []string, clusteringKey []string) error {
	columnList := strings.Join(columns, ", ")

	pkList := strings.Join(primaryKey, ", ")
	if len(clusteringKey) > 0 {
		pkList = fmt.Sprintf("(%s), %s", pkList, strings.Join(clusteringKey, ", "))
	}

	query := fmt.Sprintf(`
		CREATE MATERIALIZED VIEW IF NOT EXISTS %s AS
		SELECT %s FROM %s
		WHERE %s IS NOT NULL
		PRIMARY KEY (%s)
	`, viewName, columnList, baseTable, primaryKey[0], pkList)

	if err := c.session.Query(query).Exec(); err != nil {
		return fmt.Errorf("failed to create materialized view %s: %w", viewName, err)
	}

	if c.logger != nil {
		c.logger.Info("Materialized view created", "view", viewName, "base_table", baseTable)
	}

	return nil
}

// SuggestIndexes 建議需要的索引（基於查詢模式）
func (c *CassandraDB) SuggestIndexes(table string, commonQueries []string) []string {
	suggestions := []string{}

	for _, query := range commonQueries {
		upperQuery := strings.ToUpper(query)

		// 檢查是否有 WHERE 子句但沒有使用主鍵
		if strings.Contains(upperQuery, "WHERE") && !strings.Contains(upperQuery, "ALLOW FILTERING") {
			// 解析 WHERE 子句中的欄位
			whereIdx := strings.Index(upperQuery, "WHERE")
			whereClause := upperQuery[whereIdx+5:]

			// 簡單解析（實際應用中可能需要更複雜的解析）
			fields := extractFieldsFromWhere(whereClause)

			for _, field := range fields {
				suggestion := fmt.Sprintf("CREATE INDEX ON %s (%s)", table, strings.ToLower(field))
				suggestions = append(suggestions, suggestion)
			}
		}
	}

	return suggestions
}

// extractFieldsFromWhere 從WHERE子句提取欄位名
func extractFieldsFromWhere(whereClause string) []string {
	fields := []string{}

	// 簡單的欄位提取邏輯
	tokens := strings.Fields(whereClause)
	for i, token := range tokens {
		if i > 0 && (tokens[i-1] == "=" || tokens[i-1] == ">" ||
			tokens[i-1] == "<" || tokens[i-1] == ">=" ||
			tokens[i-1] == "<=" || tokens[i-1] == "IN") {
			continue
		}

		if strings.Contains(token, "=") || strings.Contains(token, ">") ||
			strings.Contains(token, "<") {
			field := strings.Split(token, "=")[0]
			field = strings.Split(field, ">")[0]
			field = strings.Split(field, "<")[0]
			fields = append(fields, strings.TrimSpace(field))
		}
	}

	return fields
}
