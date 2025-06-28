package config

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logger   LoggerConfig   `mapstructure:"logger"`
}

type ServerConfig struct {
	Protocol             string    `mapstructure:"protocol"` // http1, http2, http3
	Addr                 string    `mapstructure:"addr"`
	ReadTimeout          int       `mapstructure:"read_timeout"`
	WriteTimeout         int       `mapstructure:"write_timeout"`
	IdleTimeout          int       `mapstructure:"idle_timeout"`
	KeepAlive            int       `mapstructure:"keep_alive"`
	MaxHandlers          int       `mapstructure:"max_handlers"`
	MaxConcurrentStreams uint32    `mapstructure:"max_concurrent_streams"`
	MaxReadFrameSize     uint32    `mapstructure:"max_read_frame_size"`
	TLS                  TLSConfig `mapstructure:"tls"`
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
