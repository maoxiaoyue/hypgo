package cassandra

import (
	stdcontext "context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/database"
)

// CassandraPlugin Cassandra 數據庫插件
type CassandraPlugin struct {
	config  *Config
	session *gocql.Session
	cluster *gocql.ClusterConfig
}

// Config Cassandra 配置
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
}

// NewCassandraPlugin 創建 Cassandra 插件
func NewCassandraPlugin() database.DatabasePlugin {
	return &CassandraPlugin{}
}

// Name 插件名稱
func (c *CassandraPlugin) Name() string {
	return "cassandra"
}

// Init 初始化插件配置
func (c *CassandraPlugin) Init(configMap map[string]interface{}) error {
	config := &Config{}

	// 解析配置（實際應用中可以使用 mapstructure 或其他工具）
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

	// 設置默認值
	if len(config.Hosts) == 0 {
		config.Hosts = []string{"127.0.0.1"}
	}
	if config.Consistency == "" {
		config.Consistency = "QUORUM"
	}
	if config.NumConns == 0 {
		config.NumConns = 2
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.PageSize == 0 {
		config.PageSize = 5000
	}
	if config.Timeout == 0 {
		config.Timeout = 600
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 600
	}

	c.config = config

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
			// LZ4 壓縮需要額外的導入，如需使用請確保已安裝：
			// go get github.com/gocql/gocql/lz4
			// 然後取消下面的註釋：
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

	// Shard-aware 配置（ScyllaDB）
	if config.EnableShardAware {
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(
			gocql.RoundRobinHostPolicy(),
		)
		if config.ShardAwarePort > 0 {
			cluster.Port = config.ShardAwarePort
		}
	}

	c.cluster = cluster

	return nil
}

// Connect 建立連接
func (c *CassandraPlugin) Connect() error {
	if c.cluster == nil {
		return fmt.Errorf("plugin not initialized")
	}

	session, err := c.cluster.CreateSession()
	if err != nil {
		return fmt.Errorf("failed to create cassandra session: %w", err)
	}

	c.session = session
	return nil
}

// Close 關閉連接
func (c *CassandraPlugin) Close() error {
	if c.session != nil {
		c.session.Close()
	}
	return nil
}

// Ping 健康檢查 (使用 HypGo context)
func (c *CassandraPlugin) Ping(ctx *context.Context) error {
	if c.session == nil {
		return fmt.Errorf("no cassandra session")
	}

	query := c.session.Query("SELECT now() FROM system.local")

	// 如果有 HypGo context 且包含 Request，使用其 context
	if ctx != nil && ctx.Request != nil {
		stdCtx := ctx.Request.Context()
		query = query.WithContext(stdCtx)
	} else {
		// 否則使用標準背景 context
		stdCtx := stdcontext.Background()
		query = query.WithContext(stdCtx)
	}

	if err := query.Exec(); err != nil {
		return fmt.Errorf("cassandra ping failed: %w", err)
	}

	return nil
}

// PingWithStdContext 使用標準 context 進行健康檢查
func (c *CassandraPlugin) PingWithStdContext(ctx stdcontext.Context) error {
	if c.session == nil {
		return fmt.Errorf("no cassandra session")
	}

	if ctx == nil {
		ctx = stdcontext.Background()
	}

	query := c.session.Query("SELECT now() FROM system.local").WithContext(ctx)

	if err := query.Exec(); err != nil {
		return fmt.Errorf("cassandra ping failed: %w", err)
	}

	return nil
}

// Session 獲取 Cassandra session
func (c *CassandraPlugin) Session() *gocql.Session {
	return c.session
}

// Query 執行查詢
func (c *CassandraPlugin) Query(query string, values ...interface{}) *gocql.Query {
	if c.session == nil {
		return nil
	}
	return c.session.Query(query, values...)
}

// QueryWithContext 使用 HypGo context 執行查詢
func (c *CassandraPlugin) QueryWithContext(ctx *context.Context, query string, values ...interface{}) *gocql.Query {
	if c.session == nil {
		return nil
	}

	q := c.session.Query(query, values...)

	// 如果有 HypGo context 且包含 Request，使用其 context
	if ctx != nil && ctx.Request != nil {
		stdCtx := ctx.Request.Context()
		q = q.WithContext(stdCtx)
	}

	return q
}

// Batch 創建批處理
func (c *CassandraPlugin) Batch(batchType gocql.BatchType) *gocql.Batch {
	if c.session == nil {
		return nil
	}
	return c.session.NewBatch(batchType)
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
		return gocql.Quorum, fmt.Errorf("unknown consistency level: %s", level)
	}
}

// 註冊插件到數據庫管理器的輔助函數
func Register(db *database.Database) error {
	plugin := NewCassandraPlugin()
	return db.RegisterPlugin(plugin)
}
