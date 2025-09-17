package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config 主配置結構
type Config struct {
	Server    ServerConfig    `yaml:"server" json:"server"`
	Cassandra CassandraConfig `yaml:"cassandra" json:"cassandra"`
	Logger    LoggerConfig    `yaml:"logger" json:"logger"`
	Database  DatabaseConfig  `yaml:"database" json:"database"`
}

// ServerConfig 伺服器配置
type ServerConfig struct {
	Host         string `yaml:"host" json:"host"`
	Port         int    `yaml:"port" json:"port"`
	Mode         string `yaml:"mode" json:"mode"` // http2, http3
	ReadTimeout  int    `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout int    `yaml:"write_timeout" json:"write_timeout"`
}

// CassandraConfig Cassandra配置
type CassandraConfig struct {
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
	EnableLogging    string   `yaml:"enable_logging" json:"enable_logging"` // "true" or "false"
	PageSize         int      `yaml:"page_size" json:"page_size"`
	ShardAwarePort   int      `yaml:"shard_aware_port" json:"shard_aware_port"`
	EnableShardAware bool     `yaml:"enable_shard_aware" json:"enable_shard_aware"`
}

// LoggerConfig 日誌配置
type LoggerConfig struct {
	Level      string `yaml:"level" json:"level"`
	Output     string `yaml:"output" json:"output"`
	Filename   string `yaml:"filename" json:"filename"`
	MaxSize    int    `yaml:"max_size" json:"max_size"` // MB
	MaxAge     int    `yaml:"max_age" json:"max_age"`   // days
	MaxBackups int    `yaml:"max_backups" json:"max_backups"`
	Colorize   bool   `yaml:"colorize" json:"colorize"`
}

// DatabaseConfig 資料庫配置
type DatabaseConfig struct {
	Type string `yaml:"type" json:"type"` // mysql, postgresql, cassandra, scylladb
}

// LoadConfig 載入配置
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigType("yaml")

	// 讀取主配置文件
	mainConfig, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(mainConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// 載入其他配置文件
	configDir := filepath.Dir(configPath)
	files, err := filepath.Glob(filepath.Join(configDir, "*.yaml"))
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file == configPath {
			continue // 跳過主配置文件
		}

		content, err := ioutil.ReadFile(file)
		if err != nil {
			continue
		}

		// 根據文件名決定配置類型
		filename := filepath.Base(file)
		switch filename {
		case "cassandra.yaml":
			var cassandraConfig CassandraConfig
			if err := yaml.Unmarshal(content, &cassandraConfig); err == nil {
				config.Cassandra = cassandraConfig
			}
		case "logger.yaml":
			var loggerConfig LoggerConfig
			if err := yaml.Unmarshal(content, &loggerConfig); err == nil {
				config.Logger = loggerConfig
			}
		}
	}

	// 設定預設值
	setDefaults(&config)

	return &config, nil
}

// setDefaults 設定預設值
func setDefaults(config *Config) {
	// Cassandra預設值
	if config.Cassandra.Hosts == nil || len(config.Cassandra.Hosts) == 0 {
		config.Cassandra.Hosts = []string{"127.0.0.1:9042"}
	}
	if config.Cassandra.Keyspace == "" {
		config.Cassandra.Keyspace = "hypgo"
	}
	if config.Cassandra.Consistency == "" {
		config.Cassandra.Consistency = "LOCAL_QUORUM"
	}
	if config.Cassandra.ConnectTimeout == 0 {
		config.Cassandra.ConnectTimeout = 10
	}
	if config.Cassandra.Timeout == 0 {
		config.Cassandra.Timeout = 10
	}
	if config.Cassandra.NumConns == 0 {
		config.Cassandra.NumConns = 3
	}
	if config.Cassandra.MaxRetries == 0 {
		config.Cassandra.MaxRetries = 3
	}
	if config.Cassandra.PageSize == 0 {
		config.Cassandra.PageSize = 1000
	}
	if config.Cassandra.EnableLogging == "" {
		config.Cassandra.EnableLogging = "true"
	}

	// Logger預設值
	if config.Logger.Level == "" {
		config.Logger.Level = "INFO"
	}
	if config.Logger.Output == "" {
		config.Logger.Output = "stdout"
	}
}

// DefaultCassandraConfig 預設Cassandra配置
func DefaultCassandraConfig() *CassandraConfig {
	return &CassandraConfig{
		Hosts:          []string{"127.0.0.1:9042"},
		Keyspace:       "hypgo",
		Consistency:    "LOCAL_QUORUM",
		Compression:    "snappy",
		ConnectTimeout: 10,
		Timeout:        10,
		NumConns:       3,
		MaxConns:       10,
		MaxRetries:     3,
		EnableLogging:  "true",
		PageSize:       1000,
	}
}
