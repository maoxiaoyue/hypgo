// @chris
package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/secret"
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
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Logger   LoggerConfig   `yaml:"logger"`

	// explicit 記錄設定檔或環境變數實際給了哪些鍵（"server.read_timeout"）：
	// 給了 0 是「關閉」，沒給才套用預設值。直接建構的 Config 沒有這份紀錄，0 一律視為沒給
	explicit map[string]bool
	warnings []Warning
}

// Warnings 回傳載入時發現的警告（未知的鍵、空檔案）。manifest 會把它們寫進 lint 區段
func (c *Config) Warnings() []Warning {
	return append([]Warning(nil), c.warnings...)
}

func (c *Config) markExplicit(key string) {
	if c.explicit == nil {
		c.explicit = map[string]bool{}
	}
	c.explicit[key] = true
}

// IsExplicit 回報某個鍵（如 "server.shutdown_timeout"）是否在設定檔或環境變數裡明確寫了
func (c *Config) IsExplicit(key string) bool {
	return c.explicit[key]
}

// unset 回報某個數值鍵是否「沒給」：值為 0 而且設定檔與環境變數都沒有寫它
func (c *Config) unset(key string, value int) bool {
	return value == 0 && !c.explicit[key]
}

type ServerConfig struct {
	Addr     string    `yaml:"addr"`
	Protocol string    `yaml:"protocol"` // "http1", "http2", "http3", "auto"
	TLS      TLSConfig `yaml:"tls"`
	// 秒。曾為 time.Duration：預設值 30*time.Second 再被 server 乘上
	// time.Second 後溢位 int64 成負數，http.Server 視負值為「無 timeout」，
	// 等於預設路徑完全沒有讀寫 deadline（slow-loris 攻擊面）。
	// 改用 int 秒與 IdleTimeout 及 config.yaml 範本（read_timeout: 30）語義一致
	ReadTimeout  int `yaml:"read_timeout"`
	WriteTimeout int `yaml:"write_timeout"`

	// HTTP/2 相關配置
	MaxHandlers          int `yaml:"max_handlers"`
	MaxConcurrentStreams int `yaml:"max_concurrent_streams"`
	MaxReadFrameSize     int `yaml:"max_read_frame_size"`
	IdleTimeout          int `yaml:"idle_timeout"` // 秒

	// MaxBodySize 請求 body 的大小上限（預設 10MB；寫法同 logger.rotation.max_size：100KB、10MB、1GB）。
	// 超過時讀取端收到錯誤，c.BindInput 回 413。必須大於 0，沒有「不限」的設定；
	// 個別路由以 c.SetBodyLimit 或 middleware.BodyLimit 調整
	MaxBodySize string `yaml:"max_body_size"`

	// ShutdownTimeout 優雅關閉等待進行中請求的上限（秒，預設 30）；到期後中斷剩餘連線。
	// 明確寫 0 代表不等待
	ShutdownTimeout int `yaml:"shutdown_timeout"`

	// AltSvcPort Alt-Svc 廣告的 HTTP/3 埠（預設＝實際綁定的 UDP 埠）。
	// 經埠對映（容器 -p 443:8443）對外的埠與本機不同時設定
	AltSvcPort int `yaml:"alt_svc_port"`

	// 優雅重啟
	EnableGracefulRestart bool `yaml:"enable_graceful_restart"`

	// TrustedProxies 可信代理網段（CIDR 或單一 IP，如 "10.0.0.0/8"、"127.0.0.1"）。
	// 只有直連來源落在此清單時，c.ClientIP() 才採信 X-Forwarded-For / X-Real-IP。
	// 留空（預設）＝ 不信任任何代理，ClientIP 一律回傳實際連線來源。
	// 服務位於 nginx / LB 之後時務必設定，否則 ClientIP 會是代理的位址。
	TrustedProxies []string `yaml:"trusted_proxies"`

	// CORS 預設中間件裡的 CORS（server.Start() 自動套用的那一個）。
	// 先前寫死為任何來源，使用者再掛自己的 CORS 也收不緊：preflight 被預設的先回掉
	CORS CORSConfig `yaml:"cors"`

	// DisableAutoSync 關閉啟動時自動產生 .hyp/context.yaml（預設 false＝開啟）。
	// 正式環境建議設為 true：不寫檔、不呼叫 LLM，唯讀檔案系統也不會出現寫入警告
	DisableAutoSync bool `yaml:"disable_auto_sync"`
}

// CORSConfig 對應 server.cors。整段沒寫時為任何來源（"*"）、不帶憑證。
//
//	server:
//	  cors:
//	    allow_origins: ["https://app.example.com"]
//	    allow_credentials: true
//
// allow_origins: [] 表示不掛預設的 CORS（自行 r.Use(middleware.CORS(...))，或完全不要 CORS）。
// allow_methods / allow_headers 沒寫時用預設清單。"*" 不能與 allow_credentials 併用，
// 來源須為 scheme://host[:port]；寫錯時啟動失敗
type CORSConfig struct {
	AllowOrigins     []string `yaml:"allow_origins"`
	AllowMethods     []string `yaml:"allow_methods"`
	AllowHeaders     []string `yaml:"allow_headers"`
	ExposeHeaders    []string `yaml:"expose_headers"`
	AllowCredentials bool     `yaml:"allow_credentials"`
	MaxAge           int      `yaml:"max_age"` // preflight 快取秒數；0 不送
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

// ReplicaConfig 讀取副本配置
type ReplicaConfig struct {
	DSN          string `yaml:"dsn"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
	MaxOpenConns int    `yaml:"max_open_conns"`
}

// ReplicaConfigProvider 讀取副本配置提供者（可選介面）
// 實現此介面即可啟用讀寫分離；不實現則自動回退到主庫
type ReplicaConfigProvider interface {
	GetReplicas() []ReplicaConfig
}

type DatabaseConfig struct {
	Driver       string `yaml:"driver"` // mysql, postgresql, tidb, redis
	DSN          string `yaml:"dsn"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	// Redis 配置
	Redis RedisConfig `yaml:"redis"`
	// 讀取副本配置（讀寫分離）
	Replicas []ReplicaConfig `yaml:"replicas"`
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
	Level    string               `yaml:"level"` // debug, info, notice, warning, emergency
	Output   string               `yaml:"output"`
	Colors   bool                 `yaml:"colors"`
	Rotation LoggerRotationConfig `yaml:"rotation"`
}

// LoggerRotationConfig 日誌輪轉設定（對應 config.yaml 的 logger.rotation 區段；
// Output 為檔案路徑時生效，由 pkg/logger 的 Rotation writer 在寫入路徑自動輪轉）
type LoggerRotationConfig struct {
	MaxSize    string `yaml:"max_size"`    // 如 "100MB"、"1GB"
	MaxAge     string `yaml:"max_age"`     // 如 "12h"、"7d"、"1w"
	MaxBackups int    `yaml:"max_backups"` // 保留備份數
	Compress   bool   `yaml:"compress"`    // 備份是否 gzip
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

	source := configFile
	if source == "" {
		source = filepath.Join(cl.configPath, "config.yaml")
	}

	// 未知的鍵、空檔案：警告（嚴格模式回錯）。config.yaml 的頂層允許使用者自己的區段
	warnings, explicit := inspectYAML(source, configData, config, true)
	if err := reportWarnings(warnings); err != nil {
		return err
	}
	if cfg, ok := config.(*Config); ok {
		cfg.warnings, cfg.explicit = warnings, explicit
		// 環境變數覆寫（HYPGO_SERVER_ADDR…）
		if err := cfg.ApplyEnv(); err != nil {
			return err
		}
	}

	// 展開 ${secret:name}（值來自 key.secret，見 pkg/secret）；沒有引用時不碰金鑰
	if err := secret.ExpandConfig(config, source); err != nil {
		return err
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

// LoadConfig 讀取設定檔，套用預設值並驗證後回傳 *Config（便捷函式）。
// 檔案不存在、格式錯誤或驗證失敗時回傳 error；成功時預設值已套用。
//
//	cfg, err := config.LoadConfig("config.yaml")
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{}
	if err := NewConfigLoader(path).Load(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ApplyDefaults 正規化設定值並套用預設值。
//
// 數值欄位：負數一律當成 0。逾時（read / write / idle_timeout）、max_handlers 與
// logger.rotation.max_backups 在設定檔或環境變數明確給 0（或負數）時代表「關閉 / 不限」，
// 沒給才用預設值。先前 0 一律被改回預設值而無法關閉，負的逾時則原樣傳給 http.Server，
// 等於沒有逾時卻沒有任何訊息。其餘數值欄位（連線池、HTTP/2 參數）的 0 仍代表使用預設值
func (c *Config) ApplyDefaults() {
	c.normalize()

	// Server 預設值
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Server.Protocol == "" {
		c.Server.Protocol = "http2"
	}
	if c.unset("server.read_timeout", c.Server.ReadTimeout) {
		c.Server.ReadTimeout = 30 // 秒
	}
	if c.unset("server.write_timeout", c.Server.WriteTimeout) {
		c.Server.WriteTimeout = 30 // 秒
	}

	// HTTP/2 預設值
	if c.unset("server.max_handlers", c.Server.MaxHandlers) {
		c.Server.MaxHandlers = 1000
	}
	if c.Server.MaxConcurrentStreams == 0 {
		c.Server.MaxConcurrentStreams = 250
	}
	if c.Server.MaxReadFrameSize == 0 {
		c.Server.MaxReadFrameSize = 1048576 // 1MB
	}
	if c.unset("server.idle_timeout", c.Server.IdleTimeout) {
		c.Server.IdleTimeout = 120 // 120秒
	}
	if strings.TrimSpace(c.Server.MaxBodySize) == "" && !c.explicit["server.max_body_size"] {
		c.Server.MaxBodySize = DefaultMaxBodySize
	}
	if c.unset("server.shutdown_timeout", c.Server.ShutdownTimeout) {
		c.Server.ShutdownTimeout = 30
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
	// 讀取副本預設值：繼承主庫的連接池參數
	for i := range c.Database.Replicas {
		if c.Database.Replicas[i].MaxIdleConns == 0 {
			c.Database.Replicas[i].MaxIdleConns = c.Database.MaxIdleConns
		}
		if c.Database.Replicas[i].MaxOpenConns == 0 {
			c.Database.Replicas[i].MaxOpenConns = c.Database.MaxOpenConns
		}
	}

	// Logger 預設值
	if c.Logger.Level == "" {
		c.Logger.Level = "info"
	}
	if c.Logger.Output == "" {
		c.Logger.Output = "stdout"
	}
	if c.Logger.Rotation.MaxSize == "" {
		c.Logger.Rotation.MaxSize = "100MB"
	}
	if c.Logger.Rotation.MaxAge == "" {
		c.Logger.Rotation.MaxAge = "7d"
	}
	if c.unset("logger.rotation.max_backups", c.Logger.Rotation.MaxBackups) {
		c.Logger.Rotation.MaxBackups = 10
	}
}

// normalize 把大小寫與別名統一成內部使用的寫法，並把負數當成 0
func (c *Config) normalize() {
	c.Server.Protocol = strings.ToLower(strings.TrimSpace(c.Server.Protocol))
	if full, ok := map[string]string{"h1": "http1", "h2": "http2", "h3": "http3"}[c.Server.Protocol]; ok {
		c.Server.Protocol = full // pkg/server 一向接受的簡寫；先前在這裡就被驗證擋下
	}
	c.Logger.Level = strings.ToLower(strings.TrimSpace(c.Logger.Level))
	c.Database.Driver = canonicalDriver(c.Database.Driver)

	for _, n := range []*int{
		&c.Server.ReadTimeout, &c.Server.WriteTimeout, &c.Server.IdleTimeout, &c.Server.MaxHandlers,
		&c.Server.MaxConcurrentStreams, &c.Server.MaxReadFrameSize,
		&c.Server.ShutdownTimeout, &c.Server.AltSvcPort,
		&c.Database.MaxIdleConns, &c.Database.MaxOpenConns, &c.Logger.Rotation.MaxBackups,
	} {
		if *n < 0 {
			*n = 0
		}
	}
	for i := range c.Database.Replicas {
		if c.Database.Replicas[i].MaxIdleConns < 0 {
			c.Database.Replicas[i].MaxIdleConns = 0
		}
		if c.Database.Replicas[i].MaxOpenConns < 0 {
			c.Database.Replicas[i].MaxOpenConns = 0
		}
	}
}

// canonicalDriver 統一 driver 名稱：不分大小寫，postgresql / pg 視為 postgres
func canonicalDriver(driver string) string {
	switch d := strings.ToLower(strings.TrimSpace(driver)); d {
	case "postgresql", "pg":
		return "postgres"
	default:
		return d
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

	// 位址：先前 "8080"、任意文字都載入成功，要到啟動監聽才失敗
	if c.Server.Addr != "" {
		if err := validateAddr(c.Server.Addr); err != nil {
			return fmt.Errorf("server.addr %q: %w", c.Server.Addr, err)
		}
	}

	// 可信代理：先前寫錯一筆要到啟動才發現，而且只是一行警告、整份清單作廢
	for _, p := range c.Server.TrustedProxies {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(p); err != nil && net.ParseIP(p) == nil {
			return fmt.Errorf("server.trusted_proxies: %q is not an IP address or a CIDR range", p)
		}
	}
	if n, err := parseByteSize(c.Server.MaxBodySize); err != nil || n <= 0 {
		return fmt.Errorf("server.max_body_size %q: expected a size greater than 0 such as 512KB or 10MB (there is no unlimited setting)", c.Server.MaxBodySize)
	}
	if c.Server.AltSvcPort > 65535 {
		return fmt.Errorf("server.alt_svc_port %d: must be 0-65535", c.Server.AltSvcPort)
	}

	// 輪轉：先前 max_size: banana 載入成功，啟動時才由 logger 回錯（或退回 stdout）
	if s := c.Logger.Rotation.MaxSize; s != "" && !rotationSize.MatchString(s) {
		return fmt.Errorf("logger.rotation.max_size %q: expected a size such as 100MB or 1GB", s)
	}
	if s := c.Logger.Rotation.MaxAge; s != "" && !validRotationAge(s) {
		return fmt.Errorf("logger.rotation.max_age %q: expected a duration such as 12h, 7d or 1w", s)
	}

	// 副本一定要有 DSN
	for i, r := range c.Database.Replicas {
		if strings.TrimSpace(r.DSN) == "" {
			return fmt.Errorf("database.replicas[%d].dsn is required", i)
		}
	}

	return nil
}

// validateAddr 檢查監聽位址是 host:port（host 可省略），port 為 0–65535
func validateAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("expected host:port such as :8080 or 127.0.0.1:8080")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("port %q must be a number from 0 to 65535", port)
	}
	return nil
}

// DefaultMaxBodySize server.max_body_size 的預設值
const DefaultMaxBodySize = "10MB"

// MaxBodyBytes 回傳 max_body_size 的位元組數；沒設定或無效時（程式內建構的 Config）用預設值
func (s ServerConfig) MaxBodyBytes() int64 {
	if n, err := parseByteSize(s.MaxBodySize); err == nil && n > 0 {
		return n
	}
	n, _ := parseByteSize(DefaultMaxBodySize)
	return n
}

// parseByteSize 解析大小字串（格式同 rotationSize：數字加可選的 B / K / KB / M / MB / G / GB，1024 進位）
func parseByteSize(s string) (int64, error) {
	if !rotationSize.MatchString(s) {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := float64(1)
	for _, u := range []struct {
		suffix string
		mult   float64
	}{{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			mult, s = u.mult, strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(v * mult), nil
}

// rotationSize 與 pkg/logger 的 parseSize 接受的格式一致
var rotationSize = regexp.MustCompile(`(?i)^\s*\d+(\.\d+)?\s*(B|K|KB|M|MB|G|GB)?\s*$`)

// validRotationAge 與 pkg/logger 的 parseAge 接受的格式一致：<n>h / <n>d / <n>w，或 time.ParseDuration 的寫法
func validRotationAge(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if n := len(s); n > 1 && strings.ContainsRune("hdw", rune(s[n-1])) && strings.Trim(s[:n-1], "0123456789") == "" {
		return true
	}
	d, err := time.ParseDuration(s)
	return err == nil && d >= 0
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

// 配置驗證輔助函數

// ValidateProtocol 驗證協議
func ValidateProtocol(protocol string) error {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "http1", "http2", "http3", "auto":
		return nil
	default:
		return fmt.Errorf("invalid protocol: %s, must be http1, http2, http3, or auto", protocol)
	}
}

// ValidateLogLevel 驗證日誌級別（不分大小寫）。
// 與 pkg/logger 認得的等級一致：先前 error、warn 被拒絕，logger 本身卻支援
func ValidateLogLevel(level string) error {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "notice", "warning", "warn", "error", "emergency":
		return nil
	default:
		return fmt.Errorf("invalid log level: %s, must be debug, info, notice, warning, error or emergency", level)
	}
}

// ValidateDatabaseDriver 驗證數據庫驅動
func ValidateDatabaseDriver(driver string) error {
	switch canonicalDriver(driver) {
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
	return s.ReadTimeout
}

// GetWriteTimeout 獲取寫入超時（秒）
func (s *ServerConfig) GetWriteTimeout() int {
	return s.WriteTimeout
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

// GetReplicas 獲取讀取副本配置（實現 ReplicaConfigProvider 介面）
func (d *DatabaseConfig) GetReplicas() []ReplicaConfig {
	return d.Replicas
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

// IsColorized 是否啟用彩色輸出
func (l *LoggerConfig) IsColorized() bool {
	return l.Colors
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
