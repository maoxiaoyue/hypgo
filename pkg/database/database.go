package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/redis/go-redis/v9"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// DatabasePlugin 數據庫插件接口
type DatabasePlugin interface {
	Name() string
	Init(config map[string]interface{}) error
	Connect() error
	Close() error
	Ping(ctx context.Context) error
}

// Database 數據庫管理器
type Database struct {
	config  config.DatabaseConfigInterface
	sqlDB   *sql.DB  // 主庫（寫入）
	hypDB   *bun.DB  // 主庫 HypDB ORM 實例（寫入）
	redisDB *redis.Client

	// 讀寫分離
	replicaPool *ReplicaPool // 讀取副本池

	// 插件系統
	plugins map[string]DatabasePlugin
	mu      sync.RWMutex
}

// NewWithInterface 使用接口創建數據庫實例
func NewWithInterface(cfg config.DatabaseConfigInterface) (*Database, error) {
	if cfg == nil {
		return nil, fmt.Errorf("database config is nil")
	}

	db := &Database{
		config:  cfg,
		plugins: make(map[string]DatabasePlugin),
	}

	driver := cfg.GetDriver()
	if driver == "" {
		// 允許沒有數據庫配置
		return db, nil
	}

	switch driver {
	case "mysql", "tidb":
		return db.initMySQL()
	case "postgres":
		return db.initPostgres()
	case "redis":
		return db.initRedis()
	default:
		// 嘗試作為插件加載
		if plugin, exists := db.GetPlugin(driver); exists {
			if err := plugin.Connect(); err != nil {
				return nil, fmt.Errorf("failed to connect to %s: %w", driver, err)
			}
			return db, nil
		}
		return nil, fmt.Errorf("unsupported database driver: %s", driver)
	}
}

// New 向後兼容的創建方法（需要具體配置結構）
func New(cfg interface{}) (*Database, error) {
	// 嘗試轉換為接口
	if configInterface, ok := cfg.(config.DatabaseConfigInterface); ok {
		return NewWithInterface(configInterface)
	}

	// 為了向後兼容，提供一個適配器
	adapter := &DatabaseConfigAdapter{cfg: cfg}
	return NewWithInterface(adapter)
}

// DatabaseConfigAdapter 配置適配器（用於向後兼容）
type DatabaseConfigAdapter struct {
	cfg interface{}
}

func (a *DatabaseConfigAdapter) GetDriver() string {
	// 使用反射獲取 Driver 欄位
	if cfg, ok := a.cfg.(struct {
		Driver string
	}); ok {
		return cfg.Driver
	}
	return ""
}

func (a *DatabaseConfigAdapter) GetDSN() string {
	if cfg, ok := a.cfg.(struct {
		DSN string
	}); ok {
		return cfg.DSN
	}
	return ""
}

func (a *DatabaseConfigAdapter) GetMaxIdleConns() int {
	if cfg, ok := a.cfg.(struct {
		MaxIdleConns int
	}); ok {
		return cfg.MaxIdleConns
	}
	return 10
}

func (a *DatabaseConfigAdapter) GetMaxOpenConns() int {
	if cfg, ok := a.cfg.(struct {
		MaxOpenConns int
	}); ok {
		return cfg.MaxOpenConns
	}
	return 100
}

func (a *DatabaseConfigAdapter) GetRedisConfig() config.RedisConfigInterface {
	// 簡化的 Redis 配置適配
	return &RedisConfigAdapter{cfg: a.cfg}
}

// GetReplicas 向後兼容：適配器不支持讀取副本
func (a *DatabaseConfigAdapter) GetReplicas() []config.ReplicaConfig {
	return nil
}

// RedisConfigAdapter Redis配置適配器
type RedisConfigAdapter struct {
	cfg interface{}
}

func (r *RedisConfigAdapter) GetAddr() string {
	// 嘗試從嵌套結構獲取
	type redisConfig struct {
		Redis struct {
			Addr string
		}
	}
	if cfg, ok := r.cfg.(redisConfig); ok {
		return cfg.Redis.Addr
	}
	return "localhost:6379"
}

func (r *RedisConfigAdapter) GetPassword() string {
	type redisConfig struct {
		Redis struct {
			Password string
		}
	}
	if cfg, ok := r.cfg.(redisConfig); ok {
		return cfg.Redis.Password
	}
	return ""
}

func (r *RedisConfigAdapter) GetDB() int {
	type redisConfig struct {
		Redis struct {
			DB int
		}
	}
	if cfg, ok := r.cfg.(redisConfig); ok {
		return cfg.Redis.DB
	}
	return 0
}

// initMySQL 初始化 MySQL/TiDB 連接
func (d *Database) initMySQL() (*Database, error) {
	dsn := d.config.GetDSN()
	if dsn == "" {
		return nil, fmt.Errorf("MySQL DSN is required")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql: %w", err)
	}

	// 設置連接池參數
	maxIdleConns := d.config.GetMaxIdleConns()
	if maxIdleConns > 0 {
		db.SetMaxIdleConns(maxIdleConns)
	}

	maxOpenConns := d.config.GetMaxOpenConns()
	if maxOpenConns > 0 {
		db.SetMaxOpenConns(maxOpenConns)
	}

	// 測試連接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	d.sqlDB = db
	d.hypDB = bun.NewDB(db, mysqldialect.New())

	// 初始化讀取副本
	if err := d.initReplicas(); err != nil {
		return nil, err
	}

	return d, nil
}

// initPostgres 初始化 PostgreSQL 連接
func (d *Database) initPostgres() (*Database, error) {
	dsn := d.config.GetDSN()
	if dsn == "" {
		return nil, fmt.Errorf("PostgreSQL DSN is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres: %w", err)
	}

	// 設置連接池參數
	maxIdleConns := d.config.GetMaxIdleConns()
	if maxIdleConns > 0 {
		db.SetMaxIdleConns(maxIdleConns)
	}

	maxOpenConns := d.config.GetMaxOpenConns()
	if maxOpenConns > 0 {
		db.SetMaxOpenConns(maxOpenConns)
	}

	// 測試連接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	d.sqlDB = db
	d.hypDB = bun.NewDB(db, pgdialect.New())

	// 初始化讀取副本
	if err := d.initReplicas(); err != nil {
		return nil, err
	}

	return d, nil
}

// initReplicas 初始化讀取副本（在主庫初始化後調用）
// 使用可選介面 ReplicaConfigProvider 進行型別斷言，保持向後兼容
func (d *Database) initReplicas() error {
	provider, ok := d.config.(config.ReplicaConfigProvider)
	if !ok {
		return nil // 配置不支持讀取副本，跳過
	}

	replicas := provider.GetReplicas()
	if len(replicas) == 0 {
		return nil // 無副本配置
	}

	driver := d.config.GetDriver()
	d.replicaPool = NewReplicaPool()

	for i, replicaCfg := range replicas {
		replica, err := initReplica(driver, replicaCfg)
		if err != nil {
			// 關閉已初始化的副本
			d.replicaPool.Close()
			d.replicaPool = nil
			return fmt.Errorf("failed to init read replica %d: %w", i, err)
		}
		d.replicaPool.Add(replica)
	}

	return nil
}

// initRedis 初始化 Redis 連接
func (d *Database) initRedis() (*Database, error) {
	redisConfig := d.config.GetRedisConfig()
	if redisConfig == nil {
		return nil, fmt.Errorf("Redis configuration is required")
	}

	addr := redisConfig.GetAddr()
	if addr == "" {
		addr = "localhost:6379"
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: redisConfig.GetPassword(),
		DB:       redisConfig.GetDB(),
	})

	// 測試連接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	d.redisDB = client

	return d, nil
}

// RegisterPlugin 註冊數據庫插件
func (d *Database) RegisterPlugin(plugin DatabasePlugin) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	name := plugin.Name()
	if _, exists := d.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	d.plugins[name] = plugin
	return nil
}

// GetPlugin 獲取插件
func (d *Database) GetPlugin(name string) (DatabasePlugin, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	plugin, exists := d.plugins[name]
	return plugin, exists
}

// LoadPlugin 動態加載插件
func (d *Database) LoadPlugin(name string, config map[string]interface{}) error {
	plugin, exists := d.GetPlugin(name)
	if !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	if err := plugin.Init(config); err != nil {
		return fmt.Errorf("failed to init plugin %s: %w", name, err)
	}

	if err := plugin.Connect(); err != nil {
		return fmt.Errorf("failed to connect plugin %s: %w", name, err)
	}

	return nil
}

// HypDB 獲取主庫的 HypDB ORM 數據庫實例（始終返回主庫，保持向後兼容）
// 如需讀寫分離，請使用 ReadHypDB() / WriteHypDB()
func (d *Database) HypDB() *bun.DB {
	return d.hypDB
}

// ReadHypDB 獲取讀取用的 HypDB ORM 實例
// 如果有配置讀取副本，則從副本池中輪詢選擇；否則回退到主庫
func (d *Database) ReadHypDB() *bun.DB {
	if d.replicaPool != nil {
		if db := d.replicaPool.Next(); db != nil {
			return db
		}
	}
	return d.hypDB
}

// WriteHypDB 獲取寫入用的 HypDB ORM 實例（始終返回主庫）
func (d *Database) WriteHypDB() *bun.DB {
	return d.hypDB
}

// Redis 獲取 Redis 客戶端
func (d *Database) Redis() *redis.Client {
	return d.redisDB
}

// SQL 獲取主庫的原始 SQL 數據庫連接（始終返回主庫，保持向後兼容）
// 如需讀寫分離，請使用 ReadSQL() / WriteSQL()
func (d *Database) SQL() *sql.DB {
	return d.sqlDB
}

// ReadSQL 獲取讀取用的原始 SQL 連接
// 如果有配置讀取副本，則從副本池中輪詢選擇；否則回退到主庫
func (d *Database) ReadSQL() *sql.DB {
	if d.replicaPool != nil {
		if db := d.replicaPool.NextSQL(); db != nil {
			return db
		}
	}
	return d.sqlDB
}

// WriteSQL 獲取寫入用的原始 SQL 連接（始終返回主庫）
func (d *Database) WriteSQL() *sql.DB {
	return d.sqlDB
}

// HasReplicas 檢查是否有配置讀取副本
func (d *Database) HasReplicas() bool {
	return d.replicaPool != nil && d.replicaPool.Len() > 0
}

// ReplicaCount 獲取讀取副本數量
func (d *Database) ReplicaCount() int {
	if d.replicaPool == nil {
		return 0
	}
	return d.replicaPool.Len()
}

// Close 關閉數據庫連接
// 注意：hypDB.Close() 會關閉底層 sql.DB，因此不需要重複關閉
// 關閉順序：讀取副本 → 主庫 → Redis → 插件
func (d *Database) Close() error {
	var errs []error

	// 先關閉讀取副本池
	if d.replicaPool != nil {
		if replicaErrs := d.replicaPool.Close(); len(replicaErrs) > 0 {
			errs = append(errs, replicaErrs...)
		}
		d.replicaPool = nil
	}

	// 關閉主庫 HypDB（會同時關閉底層 sql.DB）
	if d.hypDB != nil {
		if err := d.hypDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close HypDB database: %w", err))
		}
	} else if d.sqlDB != nil {
		// 僅在沒有 hypDB 時直接關閉 sqlDB（例如 Redis-only 模式）
		if err := d.sqlDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close SQL database: %w", err))
		}
	}

	// 關閉 Redis 連接
	if d.redisDB != nil {
		if err := d.redisDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close Redis: %w", err))
		}
	}

	// 關閉所有插件
	d.mu.RLock()
	defer d.mu.RUnlock()
	for name, plugin := range d.plugins {
		if err := plugin.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close plugin %s: %w", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing database connections: %v", errs)
	}

	return nil
}

// IsConnected 檢查數據庫是否已連接
func (d *Database) IsConnected() bool {
	ctx := context.Background()

	if d.sqlDB != nil {
		return d.sqlDB.Ping() == nil
	}
	if d.redisDB != nil {
		return d.redisDB.Ping(ctx).Err() == nil
	}

	// 檢查插件連接狀態
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, plugin := range d.plugins {
		if err := plugin.Ping(ctx); err == nil {
			return true
		}
	}

	return false
}

// Type 獲取數據庫類型
func (d *Database) Type() string {
	if d.config != nil {
		return d.config.GetDriver()
	}
	return ""
}

// Transaction 執行事務（僅支持 SQL 數據庫，使用原始 sql.Tx）
// 接受任何 context.Context，包括 HypGo *context.Context（因其實現 context.Context 介面）
func (d *Database) Transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	if d.sqlDB == nil {
		return fmt.Errorf("no SQL database connection")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	tx, err := d.sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction failed: %v, rollback failed: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// HypDBTransaction 使用 HypDB ORM 執行事務
// 透過 bun.Tx 提供完整的 ORM 查詢能力
// 接受任何 context.Context，包括 HypGo *context.Context
func (d *Database) HypDBTransaction(ctx context.Context, fn func(context.Context, bun.Tx) error) error {
	if d.hypDB == nil {
		return fmt.Errorf("no HypDB database connection")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return d.hypDB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, tx)
	})
}

// HealthCheck 健康檢查（主庫 + 讀取副本 + Redis + 插件）
// 接受任何 context.Context，包括 HypGo *context.Context
func (d *Database) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// 檢查主庫
	if d.sqlDB != nil {
		if err := d.sqlDB.PingContext(ctx); err != nil {
			return fmt.Errorf("SQL database unhealthy: %w", err)
		}
	}

	// 檢查讀取副本
	if d.replicaPool != nil {
		if replicaErrs := d.replicaPool.PingAll(); len(replicaErrs) > 0 {
			return fmt.Errorf("read replicas unhealthy: %v", replicaErrs)
		}
	}

	// 檢查 Redis
	if d.redisDB != nil {
		if err := d.redisDB.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("Redis unhealthy: %w", err)
		}
	}

	// 檢查插件健康狀態
	d.mu.RLock()
	defer d.mu.RUnlock()
	for name, plugin := range d.plugins {
		if err := plugin.Ping(ctx); err != nil {
			return fmt.Errorf("plugin %s unhealthy: %w", name, err)
		}
	}

	return nil
}
