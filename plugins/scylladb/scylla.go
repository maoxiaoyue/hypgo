// hypgo/pkg/plugins/scylladb/scylladb.go
package scylladb

import (
	stdcontext "context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/database"
)

// ScyllaDBPlugin ScyllaDB 數據庫插件
type ScyllaDBPlugin struct {
	config  *Config
	session *gocql.Session
	cluster *gocql.ClusterConfig
}

// Config ScyllaDB 配置
type Config struct {
	Hosts            []string `yaml:"hosts" json:"hosts"`
	Keyspace         string   `yaml:"keyspace" json:"keyspace"`
	Username         string   `yaml:"username" json:"username"`
	Password         string   `yaml:"password" json:"password"`
	Consistency      string   `yaml:"consistency" json:"consistency"`
	Compression      string   `yaml:"compression" json:"compression"`
	ConnectTimeout   int      `yaml:"connect_timeout" json:"connect_timeout"`
	Timeout          int      `yaml:"timeout" json:"timeout"`
	NumConns         int      `yaml:"num_conns" json:"num_conns"`
	MaxConns         int      `yaml:"max_conns" json:"max_conns"`
	MaxRetries       int      `yaml:"max_retries" json:"max_retries"`
	EnableLogging    string   `yaml:"enable_logging" json:"enable_logging"`
	PageSize         int      `yaml:"page_size" json:"page_size"`
	ShardAwarePort   int      `yaml:"shard_aware_port" json:"shard_aware_port"`
	EnableShardAware bool     `yaml:"enable_shard_aware" json:"enable_shard_aware"`

	// ScyllaDB 特定配置
	EnableHostnameVerification bool `yaml:"enable_hostname_verification" json:"enable_hostname_verification"`
	DisableSkipMetadata        bool `yaml:"disable_skip_metadata" json:"disable_skip_metadata"`
}

// NewScyllaDBPlugin 創建 ScyllaDB 插件
func NewScyllaDBPlugin() database.DatabasePlugin {
	return &ScyllaDBPlugin{}
}

// Name 插件名稱
func (s *ScyllaDBPlugin) Name() string {
	return "scylladb"
}

// Init 初始化插件配置
func (s *ScyllaDBPlugin) Init(configMap map[string]interface{}) error {
	config := &Config{}

	// 解析配置
	if hosts, ok := configMap["hosts"].([]interface{}); ok {
		for _, host := range hosts {
			if h, ok := host.(string); ok {
				config.Hosts = append(config.Hosts, h)
			}
		}
	}

	if keyspace, ok := configMap["keyspace"].(string); ok {
		config.Keyspace = keyspace
	}

	if username, ok := configMap["username"].(string); ok {
		config.Username = username
	}

	if password, ok := configMap["password"].(string); ok {
		config.Password = password
	}

	if consistency, ok := configMap["consistency"].(string); ok {
		config.Consistency = consistency
	}

	if compression, ok := configMap["compression"].(string); ok {
		config.Compression = compression
	}

	if connectTimeout, ok := configMap["connect_timeout"].(int); ok {
		config.ConnectTimeout = connectTimeout
	}

	if timeout, ok := configMap["timeout"].(int); ok {
		config.Timeout = timeout
	}

	if numConns, ok := configMap["num_conns"].(int); ok {
		config.NumConns = numConns
	}

	if maxConns, ok := configMap["max_conns"].(int); ok {
		config.MaxConns = maxConns
	}

	if maxRetries, ok := configMap["max_retries"].(int); ok {
		config.MaxRetries = maxRetries
	}

	if pageSize, ok := configMap["page_size"].(int); ok {
		config.PageSize = pageSize
	}

	if shardAwarePort, ok := configMap["shard_aware_port"].(int); ok {
		config.ShardAwarePort = shardAwarePort
	}

	if enableShardAware, ok := configMap["enable_shard_aware"].(bool); ok {
		config.EnableShardAware = enableShardAware
	}

	if enableHostnameVerification, ok := configMap["enable_hostname_verification"].(bool); ok {
		config.EnableHostnameVerification = enableHostnameVerification
	}

	if disableSkipMetadata, ok := configMap["disable_skip_metadata"].(bool); ok {
		config.DisableSkipMetadata = disableSkipMetadata
	}

	// ScyllaDB 特定的默認值
	if len(config.Hosts) == 0 {
		config.Hosts = []string{"127.0.0.1"}
	}
	if config.Consistency == "" {
		config.Consistency = "LOCAL_QUORUM" // ScyllaDB 推薦使用 LOCAL_QUORUM
	}
	if config.NumConns == 0 {
		config.NumConns = 4 // ScyllaDB 推薦更高的連接數
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.PageSize == 0 {
		config.PageSize = 5000
	}
	if config.Timeout == 0 {
		config.Timeout = 1000 // ScyllaDB 通常有更好的響應時間
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 1000
	}
	if config.Compression == "" {
		config.Compression = "snappy" // ScyllaDB 默認使用 snappy 壓縮
	}
	// ScyllaDB 默認啟用 Shard-aware
	if !config.EnableShardAware {
		config.EnableShardAware = true
	}

	s.config = config

	// 創建集群配置
	cluster := gocql.NewCluster(config.Hosts...)
	cluster.Keyspace = config.Keyspace

	// 設置認證
	if config.Username != "" && config.Password != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: config.Username,
			Password: config.Password,
		}
	}

	// 設置一致性級別
	consistency, err := parseConsistency(config.Consistency)
	if err != nil {
		return fmt.Errorf("invalid consistency level: %w", err)
	}
	cluster.Consistency = consistency

	// 設置壓縮
	if config.Compression != "" && config.Compression != "none" {
		switch config.Compression {
		case "snappy":
			cluster.Compressor = gocql.SnappyCompressor{}
			// LZ4 壓縮需要額外的導入：
			// go get github.com/gocql/gocql/lz4
			// case "lz4":
			//	cluster.Compressor = gocql.LZ4Compressor{}
		}
	}

	// 設置超時
	cluster.ConnectTimeout = time.Duration(config.ConnectTimeout) * time.Millisecond
	cluster.Timeout = time.Duration(config.Timeout) * time.Millisecond

	// 設置連接池
	cluster.NumConns = config.NumConns

	// 設置重試策略
	cluster.RetryPolicy = &gocql.SimpleRetryPolicy{
		NumRetries: config.MaxRetries,
	}

	// 設置分頁大小
	cluster.PageSize = config.PageSize

	// ScyllaDB 特定的 Shard-aware 配置
	if config.EnableShardAware {
		// 使用 TokenAware 策略以實現 shard-aware
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
			gocql.DCAwareRoundRobinPolicy(""), // 使用 DC-aware 策略
		)

		// 如果指定了 shard-aware port，使用它
		if config.ShardAwarePort > 0 {
			cluster.Port = config.ShardAwarePort
		} else {
			cluster.Port = 19042 // ScyllaDB 默認的 shard-aware port
		}
	}

	// ScyllaDB 特定優化
	cluster.ProtoVersion = 4 // ScyllaDB 支援 CQL v4

	// 禁用初始主機查找（ScyllaDB 不需要）
	if !config.DisableSkipMetadata {
		cluster.DisableInitialHostLookup = true
	}

	s.cluster = cluster

	return nil
}

// Connect 建立連接
func (s *ScyllaDBPlugin) Connect() error {
	if s.cluster == nil {
		return fmt.Errorf("plugin not initialized")
	}

	session, err := s.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to create scylladb session: %w", err)
	}

	s.session = session
	return nil
}

// Close 關閉連接
func (s *ScyllaDBPlugin) Close() error {
	if s.session != nil {
		s.session.Close()
		s.session = nil
	}
	return nil
}

// Ping 健康檢查 (使用 HypGo context)
func (s *ScyllaDBPlugin) Ping(ctx *context.Context) error {
	if s.session == nil {
		return fmt.Errorf("no scylladb session")
	}

	// ScyllaDB 特定的健康檢查查詢
	query := s.session.Query("SELECT release_version FROM system.local")

	// 如果有 HypGo context 且包含 Request，使用其 context
	if ctx != nil && ctx.Request != nil {
		stdCtx := ctx.Request.Context()
		query = query.WithContext(stdCtx)
	} else {
		// 否則使用標準背景 context
		stdCtx := stdcontext.Background()
		query = query.WithContext(stdCtx)
	}

	var version string
	if err := query.Scan(&version); err != nil {
		return fmt.Errorf("scylladb ping failed: %w", err)
	}

	// 檢查是否真的是 ScyllaDB
	// ScyllaDB 版本通常包含 "Scylla" 字串
	// 這是可選的檢查，可以根據需要啟用

	return nil
}

// PingWithStdContext 使用標準 context 進行健康檢查
func (s *ScyllaDBPlugin) PingWithStdContext(ctx stdcontext.Context) error {
	if s.session == nil {
		return fmt.Errorf("no scylladb session")
	}

	if ctx == nil {
		ctx = stdcontext.Background()
	}

	query := s.session.Query("SELECT release_version FROM system.local").WithContext(ctx)

	var version string
	if err := query.Scan(&version); err != nil {
		return fmt.Errorf("scylladb ping failed: %w", err)
	}

	return nil
}

// Session 獲取 ScyllaDB session
func (s *ScyllaDBPlugin) Session() *gocql.Session {
	return s.session
}

// Query 執行查詢
func (s *ScyllaDBPlugin) Query(query string, values ...interface{}) *gocql.Query {
	if s.session == nil {
		return nil
	}
	return s.session.Query(query, values...)
}

// QueryWithContext 使用 HypGo context 執行查詢
func (s *ScyllaDBPlugin) QueryWithContext(ctx *context.Context, query string, values ...interface{}) *gocql.Query {
	if s.session == nil {
		return nil
	}

	q := s.session.Query(query, values...)

	// 如果有 HypGo context 且包含 Request，使用其 context
	if ctx != nil && ctx.Request != nil {
		stdCtx := ctx.Request.Context()
		q = q.WithContext(stdCtx)
	}

	return q
}

// Batch 創建批處理
func (s *ScyllaDBPlugin) Batch(batchType gocql.BatchType) *gocql.Batch {
	if s.session == nil {
		return nil
	}
	return s.session.NewBatch(batchType)
}

// ExecuteBatch 執行批處理
func (s *ScyllaDBPlugin) ExecuteBatch(batch *gocql.Batch) error {
	if s.session == nil {
		return fmt.Errorf("no scylladb session")
	}
	return s.session.ExecuteBatch(batch)
}

// GetClusterConfig 獲取集群配置（用於調試）
func (s *ScyllaDBPlugin) GetClusterConfig() *gocql.ClusterConfig {
	return s.cluster
}

// GetConfig 獲取配置
func (s *ScyllaDBPlugin) GetConfig() *Config {
	return s.config
}

// parseConsistency 解析一致性級別
func parseConsistency(level string) (gocql.Consistency, error) {
	switch level {
	case "ANY":
		return gocql.Any, nil
	case "ONE":
		return gocql.One, nil
	case "TWO":
		return gocql.Two, nil
	case "THREE":
		return gocql.Three, nil
	case "QUORUM":
		return gocql.Quorum, nil
	case "ALL":
		return gocql.All, nil
	case "LOCAL_QUORUM":
		return gocql.LocalQuorum, nil
	case "EACH_QUORUM":
		return gocql.EachQuorum, nil
	case "LOCAL_ONE":
		return gocql.LocalOne, nil
	default:
		return gocql.LocalQuorum, fmt.Errorf("unknown consistency level: %s", level)
	}
}

// 註冊插件到數據庫管理器的輔助函數
func Register(db *database.Database) error {
	plugin := NewScyllaDBPlugin()
	return db.RegisterPlugin(plugin)
}
