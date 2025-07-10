package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig           `mapstructure:"server"`
	Database DatabaseConfig         `mapstructure:"database"`
	Logger   LoggerConfig           `mapstructure:"logger"`
	Plugins  map[string]interface{} `mapstructure:"plugins"`
}

type ServerConfig struct {
	Protocol              string    `mapstructure:"protocol"` // http1, http2, http3
	Addr                  string    `mapstructure:"addr"`
	ReadTimeout           int       `mapstructure:"read_timeout"`
	WriteTimeout          int       `mapstructure:"write_timeout"`
	IdleTimeout           int       `mapstructure:"idle_timeout"`
	KeepAlive             int       `mapstructure:"keep_alive"`
	MaxHandlers           int       `mapstructure:"max_handlers"`
	MaxConcurrentStreams  uint32    `mapstructure:"max_concurrent_streams"`
	MaxReadFrameSize      uint32    `mapstructure:"max_read_frame_size"`
	TLS                   TLSConfig `mapstructure:"tls"`
	EnableGracefulRestart bool      `mapstructure:"enable_graceful_restart"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
}

type DatabaseConfig struct {
	Driver       string          `mapstructure:"driver"` // mysql, postgres, tidb, redis, cassandra
	DSN          string          `mapstructure:"dsn"`
	MaxIdleConns int             `mapstructure:"max_idle_conns"`
	MaxOpenConns int             `mapstructure:"max_open_conns"`
	Redis        RedisConfig     `mapstructure:"redis"`
	Cassandra    CassandraConfig `mapstructure:"cassandra"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type CassandraConfig struct {
	Hosts    []string `mapstructure:"hosts"`
	Keyspace string   `mapstructure:"keyspace"`
}

type LoggerConfig struct {
	Level    string         `mapstructure:"level"`
	Output   string         `mapstructure:"output"`
	Rotation RotationConfig `mapstructure:"rotation"`
	Colors   bool           `mapstructure:"colors"`
}

type RotationConfig struct {
	MaxSize    string `mapstructure:"max_size"` // 10MB, 100MB
	MaxAge     string `mapstructure:"max_age"`  // 1h, 1d, 1w
	MaxBackups int    `mapstructure:"max_backups"`
	Compress   bool   `mapstructure:"compress"`
}

func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetEnvPrefix("HYPGO")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 載入插件配置
	cfg.Plugins = make(map[string]interface{})
	if err := loadPluginConfigs(&cfg); err != nil {
		return nil, fmt.Errorf("failed to load plugin configs: %w", err)
	}

	return &cfg, nil
}

func loadPluginConfigs(cfg *Config) error {
	configDir := filepath.Dir(viper.ConfigFileUsed())

	// 插件配置文件列表
	pluginFiles := []string{
		"rabbitmq.yaml",
		"kafka.yaml",
		"cassandra.yaml",
		"scylladb.yaml",
		"mongodb.yaml",
		"elasticsearch.yaml",
	}

	for _, file := range pluginFiles {
		pluginPath := filepath.Join(configDir, file)

		// 檢查文件是否存在
		if _, err := ioutil.ReadFile(pluginPath); err != nil {
			continue // 跳過不存在的插件配置
		}

		// 讀取插件配置
		pluginViper := viper.New()
		pluginViper.SetConfigFile(pluginPath)

		if err := pluginViper.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		// 獲取插件名稱（去掉 .yaml 後綴）
		pluginName := strings.TrimSuffix(file, ".yaml")

		// 將插件配置添加到主配置中
		cfg.Plugins[pluginName] = pluginViper.AllSettings()
	}

	return nil
}

// GetPluginConfig 獲取特定插件的配置
func (c *Config) GetPluginConfig(pluginName string) (map[string]interface{}, bool) {
	config, ok := c.Plugins[pluginName].(map[string]interface{})
	return config, ok
}

// SavePIDFile 保存進程 ID 文件（用於熱重啟）
func SavePIDFile() error {
	pid := os.Getpid()
	return ioutil.WriteFile("hypgo.pid", []byte(fmt.Sprintf("%d", pid)), 0644)
}

// RemovePIDFile 刪除進程 ID 文件
func RemovePIDFile() {
	os.Remove("hypgo.pid")
}
