package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigInterface 配置接口，使用者需要實現此接口
type ConfigInterface interface {
	Validate() error
	GetServerConfig() ServerConfigInterface
	GetDatabaseConfig() DatabaseConfigInterface
	GetLoggerConfig() LoggerConfigInterface
}

type Config struct {
	Server   ServerConfig   `mapstructure:"server" yaml:"server"`
	Database DatabaseConfig `mapstructure:"database" yaml:"database"`
	Logger   LoggerConfig   `mapstructure:"logger" yaml:"logger"`
}

type ServerConfig struct {
	Addr         string        `mapstructure:"addr" yaml:"addr"`
	Protocol     string        `mapstructure:"protocol" yaml:"protocol"` // "http1", "http2", "http3", "auto"
	TLS          TLSConfig     `mapstructure:"tls" yaml:"tls"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout" yaml:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout" yaml:"write_timeout"`

	// HTTP/2 相關配置
	MaxHandlers          int `mapstructure:"max_handlers" yaml:"max_handlers"`
	MaxConcurrentStreams int `mapstructure:"max_concurrent_streams" yaml:"max_concurrent_streams"`
	MaxReadFrameSize     int `mapstructure:"max_read_frame_size" yaml:"max_read_frame_size"`
	IdleTimeout          int `mapstructure:"idle_timeout" yaml:"idle_timeout"` // 秒

	// 優雅重啟
	EnableGracefulRestart bool `mapstructure:"enable_graceful_restart" yaml:"enable_graceful_restart"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled"`
	CertFile string `mapstructure:"cert_file" yaml:"cert_file"`
	KeyFile  string `mapstructure:"key_file" yaml:"key_file"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `mapstructure:"addr" yaml:"addr"`
	Password string `mapstructure:"password" yaml:"password"`
	DB       int    `mapstructure:"db" yaml:"db"`
}

type DatabaseConfig struct {
	Driver       string `mapstructure:"driver" yaml:"driver"` // mysql, postgresql, tidb, redis
	DSN          string `mapstructure:"dsn" yaml:"dsn"`
	MaxIdleConns int    `mapstructure:"max_idle_conns" yaml:"max_idle_conns"`
	MaxOpenConns int    `mapstructure:"max_open_conns" yaml:"max_open_conns"`
	// Redis 配置
	Redis RedisConfig `mapstructure:"redis" yaml:"redis"`
}

// ServerConfigInterface 服務器配置接口
type ServerConfigInterface interface {
	GetAddr() string
	GetProtocol() string
	GetReadTimeout() int
	GetWriteTimeout() int
	GetMaxHeaderBytes() int
	IsGracefulRestartEnabled() bool
}

// DatabaseConfigInterface 數據庫配置接口
type DatabaseConfigInterface interface {
	GetDriver() string
	GetDSN() string
	GetMaxIdleConns() int
	GetMaxOpenConns() int
	GetRedisConfig() RedisConfigInterface
}

type LoggerConfig struct {
	Level        string `mapstructure:"level" yaml:"level"` // debug, info, notice, warning, emergency
	Output       string `mapstructure:"output" yaml:"output"`
	MaxSize      int    `mapstructure:"max_size" yaml:"max_size"`
	MaxAge       int    `mapstructure:"max_age" yaml:"max_age"`
	Compress     bool   `mapstructure:"compress" yaml:"compress"`
	ColorEnabled bool   `mapstructure:"color_enabled" yaml:"color_enabled"`
}

// RedisConfigInterface Redis配置接口
type RedisConfigInterface interface {
	GetAddr() string
	GetPassword() string
	GetDB() int
}

// LoggerConfigInterface 日誌配置接口
type LoggerConfigInterface interface {
	GetLevel() string
	GetOutput() string
	GetFormat() string
	GetFilename() string
	GetMaxSize() int
	GetMaxAge() int
	GetMaxBackups() int
	IsColorized() bool
}

// ConfigLoader 配置加載器
type ConfigLoader struct {
	configPath string
}

// NewConfigLoader 創建配置加載器
func NewConfigLoader(configPath string) *ConfigLoader {
	if configPath == "" {
		configPath = "config"
	}
	return &ConfigLoader{
		configPath: configPath,
	}
}

// Load 加載配置文件到指定的結構體
func (cl *ConfigLoader) Load(configFile string, config interface{}) error {
	var configData []byte
	var err error

	if configFile != "" {
		// 讀取指定的配置文件
		configData, err = os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read config file %s: %w", configFile, err)
		}
	} else {
		// 讀取默認配置文件
		defaultConfigFile := filepath.Join(cl.configPath, "config.yaml")
		configData, err = os.ReadFile(defaultConfigFile)
		if err != nil {
			return fmt.Errorf("failed to read default config file: %w", err)
		}
	}

	// 解析配置到用戶提供的結構體
	if err := yaml.Unmarshal(configData, config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 應用預設值
	if cfg, ok := config.(*Config); ok {
		cfg.ApplyDefaults()
	}

	// 如果配置實現了 Validate 方法，則調用驗證
	if validator, ok := config.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
	}

	return nil
}

// ApplyDefaults 應用預設值
func (c *Config) ApplyDefaults() {
	// Server 預設值
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.Protocol == "" {
		c.Server.Protocol = "http2"
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	if c.Server.WriteTimeout == 0 {
		c.Server.WriteTimeout = 30 * time.Second
	}

	// HTTP/2 預設值
	if c.Server.MaxHandlers == 0 {
		c.Server.MaxHandlers = 1000
	}
	if c.Server.MaxConcurrentStreams == 0 {
		c.Server.MaxConcurrentStreams = 250
	}
	if c.Server.MaxReadFrameSize == 0 {
		c.Server.MaxReadFrameSize = 1048576 // 1MB
	}
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = 120 // 120秒
	}

	// Database 預設值
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 10
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 100
	}
	if c.Database.Redis.Addr == "" {
		c.Database.Redis.Addr = "localhost:6379"
	}

	// Logger 預設值
	if c.Logger.Level == "" {
		c.Logger.Level = "info"
	}
	if c.Logger.Output == "" {
		c.Logger.Output = "stdout"
	}
	if c.Logger.MaxSize == 0 {
		c.Logger.MaxSize = 100 // 100MB
	}
	if c.Logger.MaxAge == 0 {
		c.Logger.MaxAge = 7 // 7天
	}
}

// Validate 驗證配置
func (c *Config) Validate() error {
	// 驗證協議
	if err := ValidateProtocol(c.Server.Protocol); err != nil {
		return err
	}

	// 驗證日誌級別
	if err := ValidateLogLevel(c.Logger.Level); err != nil {
		return err
	}

	// 驗證數據庫驅動
	if c.Database.Driver != "" {
		if err := ValidateDatabaseDriver(c.Database.Driver); err != nil {
			return err
		}
	}

	// 驗證 TLS 配置
	if c.Server.TLS.Enabled {
		if c.Server.TLS.CertFile == "" || c.Server.TLS.KeyFile == "" {
			return fmt.Errorf("TLS enabled but cert_file or key_file is empty")
		}
	}

	// HTTP/3 必須啟用 TLS
	if c.Server.Protocol == "http3" && !c.Server.TLS.Enabled {
		return fmt.Errorf("HTTP/3 requires TLS to be enabled")
	}

	return nil
}

// GetServerConfig 實現 ConfigInterface
func (c *Config) GetServerConfig() ServerConfigInterface {
	return &c.Server
}

// GetDatabaseConfig 實現 ConfigInterface
func (c *Config) GetDatabaseConfig() DatabaseConfigInterface {
	return &c.Database
}

// GetLoggerConfig 實現 ConfigInterface
func (c *Config) GetLoggerConfig() LoggerConfigInterface {
	return &c.Logger
}

// LoadWithInterface 加載配置並返回 ConfigInterface
func (cl *ConfigLoader) LoadWithInterface(configFile string, config ConfigInterface) error {
	if err := cl.Load(configFile, config); err != nil {
		return err
	}
	return config.Validate()
}

// LoadYAML 通用的 YAML 文件加載方法
func LoadYAML(filename string, out interface{}) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	return nil
}

// SaveYAML 通用的 YAML 文件保存方法
func SaveYAML(filename string, data interface{}) error {
	output, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal yaml: %w", err)
	}

	if err := os.WriteFile(filename, output, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}

	return nil
}

// ConfigWatcher 監視配置文件變化（用於熱重載）
type ConfigWatcher struct {
	configFile string
	onChange   func() error
}

// NewConfigWatcher 創建配置監視器
func NewConfigWatcher(configFile string, onChange func() error) *ConfigWatcher {
	return &ConfigWatcher{
		configFile: configFile,
		onChange:   onChange,
	}
}

// 配置驗證輔助函數

// ValidateProtocol 驗證協議
func ValidateProtocol(protocol string) error {
	switch protocol {
	case "http1", "http2", "http3", "auto":
		return nil
	default:
		return fmt.Errorf("invalid protocol: %s, must be http1, http2, http3, or auto", protocol)
	}
}

// ValidateLogLevel 驗證日誌級別
func ValidateLogLevel(level string) error {
	switch level {
	case "debug", "info", "notice", "warning", "emergency":
		return nil
	default:
		return fmt.Errorf("invalid log level: %s", level)
	}
}

// ValidateDatabaseDriver 驗證數據庫驅動
func ValidateDatabaseDriver(driver string) error {
	switch driver {
	case "mysql", "postgres", "tidb", "redis", "cassandra", "scylladb", "":
		return nil
	default:
		return fmt.Errorf("unsupported database driver: %s", driver)
	}
}

// ===== ServerConfig 接口實現 =====

// GetAddr 獲取服務器地址
func (s *ServerConfig) GetAddr() string {
	return s.Addr
}

// GetProtocol 獲取協議
func (s *ServerConfig) GetProtocol() string {
	return s.Protocol
}

// GetReadTimeout 獲取讀取超時（秒）
func (s *ServerConfig) GetReadTimeout() int {
	return int(s.ReadTimeout.Seconds())
}

// GetWriteTimeout 獲取寫入超時（秒）
func (s *ServerConfig) GetWriteTimeout() int {
	return int(s.WriteTimeout.Seconds())
}

// GetMaxHeaderBytes 獲取最大標頭字節數
func (s *ServerConfig) GetMaxHeaderBytes() int {
	return 1 << 20 // 1MB
}

// IsGracefulRestartEnabled 是否啟用優雅重啟
func (s *ServerConfig) IsGracefulRestartEnabled() bool {
	return s.EnableGracefulRestart
}

// ===== DatabaseConfig 接口實現 =====

// GetDriver 獲取數據庫驅動
func (d *DatabaseConfig) GetDriver() string {
	return d.Driver
}

// GetDSN 獲取數據源名稱
func (d *DatabaseConfig) GetDSN() string {
	return d.DSN
}

// GetMaxIdleConns 獲取最大空閒連接數
func (d *DatabaseConfig) GetMaxIdleConns() int {
	if d.MaxIdleConns > 0 {
		return d.MaxIdleConns
	}
	return 10 // 預設值
}

// GetMaxOpenConns 獲取最大開啟連接數
func (d *DatabaseConfig) GetMaxOpenConns() int {
	if d.MaxOpenConns > 0 {
		return d.MaxOpenConns
	}
	return 100 // 預設值
}

// GetRedisConfig 獲取 Redis 配置
func (d *DatabaseConfig) GetRedisConfig() RedisConfigInterface {
	return &d.Redis
}

// ===== LoggerConfig 接口實現 =====

// GetLevel 獲取日誌級別
func (l *LoggerConfig) GetLevel() string {
	return l.Level
}

// GetOutput 獲取輸出位置
func (l *LoggerConfig) GetOutput() string {
	return l.Output
}

// GetFormat 獲取日誌格式
func (l *LoggerConfig) GetFormat() string {
	return "json" // 預設使用 JSON 格式
}

// GetFilename 獲取日誌文件名
func (l *LoggerConfig) GetFilename() string {
	if l.Output == "file" {
		return "logs/app.log"
	}
	return ""
}

// GetMaxSize 獲取最大文件大小（MB）
func (l *LoggerConfig) GetMaxSize() int {
	return l.MaxSize
}

// GetMaxAge 獲取最大保存天數
func (l *LoggerConfig) GetMaxAge() int {
	return l.MaxAge
}

// GetMaxBackups 獲取最大備份數量
func (l *LoggerConfig) GetMaxBackups() int {
	return 10 // 預設保留 10 個備份
}

// IsColorized 是否啟用彩色輸出
func (l *LoggerConfig) IsColorized() bool {
	return l.ColorEnabled
}

// ===== RedisConfig 接口實現 =====

// GetAddr 獲取 Redis 地址
func (r *RedisConfig) GetAddr() string {
	if r.Addr == "" {
		return "localhost:6379"
	}
	return r.Addr
}

// GetPassword 獲取 Redis 密碼
func (r *RedisConfig) GetPassword() string {
	return r.Password
}

// GetDB 獲取 Redis 數據庫編號
func (r *RedisConfig) GetDB() int {
	return r.DB
}
