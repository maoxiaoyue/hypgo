package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
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
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logger   LoggerConfig   `mapstructure:"logger"`
	Plugins  PluginsConfig  `mapstructure:"plugins"`
}

type ServerConfig struct {
	Addr         string        `mapstructure:"addr"`
	Protocol     string        `mapstructure:"protocol"` // "http2", "http3"
	TLS          TLSConfig     `mapstructure:"tls"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

type DatabaseConfig struct {
	Driver string `mapstructure:"driver"` // mysql, postgresql, tidb, redis
	DSN    string `mapstructure:"dsn"`
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
	Level        string `mapstructure:"level"` // debug, info, notice, warning, emergency
	Output       string `mapstructure:"output"`
	MaxSize      int    `mapstructure:"max_size"`
	MaxAge       int    `mapstructure:"max_age"`
	Compress     bool   `mapstructure:"compress"`
	ColorEnabled bool   `mapstructure:"color_enabled"`
}

type PluginsConfig struct {
	Enabled []string               `mapstructure:"enabled"`
	Configs map[string]interface{} `mapstructure:"configs"`
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
	plugins    map[string]interface{}
}

// NewConfigLoader 創建配置加載器
func NewConfigLoader(configPath string) *ConfigLoader {
	if configPath == "" {
		configPath = "config"
	}
	return &ConfigLoader{
		configPath: configPath,
		plugins:    make(map[string]interface{}),
	}
}

// Load 加載配置文件到指定的結構體
func (cl *ConfigLoader) Load(configFile string, config interface{}) error {
	var configData []byte
	var err error

	if configFile != "" {
		// 讀取指定的配置文件
		configData, err = ioutil.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read config file %s: %w", configFile, err)
		}
	} else {
		// 讀取默認配置文件
		defaultConfigFile := filepath.Join(cl.configPath, "config.yaml")
		configData, err = ioutil.ReadFile(defaultConfigFile)
		if err != nil {
			return fmt.Errorf("failed to read default config file: %w", err)
		}
	}

	// 解析配置到用戶提供的結構體
	if err := yaml.Unmarshal(configData, config); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 加載插件配置
	if err := cl.loadPluginConfigs(); err != nil {
		return fmt.Errorf("failed to load plugin configs: %w", err)
	}

	// 如果配置實現了 Validate 方法，則調用驗證
	if validator, ok := config.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
	}

	return nil
}

// LoadWithInterface 加載配置並返回 ConfigInterface
func (cl *ConfigLoader) LoadWithInterface(configFile string, config ConfigInterface) error {
	if err := cl.Load(configFile, config); err != nil {
		return err
	}
	return config.Validate()
}

// loadPluginConfigs 加載插件配置文件
func (cl *ConfigLoader) loadPluginConfigs() error {
	configFiles, err := filepath.Glob(filepath.Join(cl.configPath, "*.yaml"))
	if err != nil {
		return err
	}

	for _, file := range configFiles {
		filename := filepath.Base(file)
		if filename == "config.yaml" {
			continue
		}

		// 讀取插件配置文件
		data, err := ioutil.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		// 解析插件配置
		var pluginConfig map[string]interface{}
		if err := yaml.Unmarshal(data, &pluginConfig); err != nil {
			return fmt.Errorf("failed to unmarshal %s: %w", file, err)
		}

		// 插件名稱為文件名（去掉.yaml後綴）
		pluginName := strings.TrimSuffix(filename, ".yaml")
		cl.plugins[pluginName] = pluginConfig
	}

	return nil
}

// GetPluginConfig 獲取插件配置
func (cl *ConfigLoader) GetPluginConfig(pluginName string) map[string]interface{} {
	if cfg, ok := cl.plugins[pluginName]; ok {
		if configMap, ok := cfg.(map[string]interface{}); ok {
			return configMap
		}
	}
	return nil
}

// GetAllPluginConfigs 獲取所有插件配置
func (cl *ConfigLoader) GetAllPluginConfigs() map[string]interface{} {
	return cl.plugins
}

// LoadYAML 通用的 YAML 文件加載方法
func LoadYAML(filename string, out interface{}) error {
	data, err := ioutil.ReadFile(filename)
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

	if err := ioutil.WriteFile(filename, output, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", filename, err)
	}

	return nil
}

// WatchConfig 監視配置文件變化（用於熱重載）
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
	case "http1", "http2", "http3":
		return nil
	default:
		return fmt.Errorf("invalid protocol: %s, must be http1, http2, or http3", protocol)
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
