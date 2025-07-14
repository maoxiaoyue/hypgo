package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install [plugin-name]",
	Short: "Add a plugin to the project",
	Long:  `Add plugins like RabbitMQ, Kafka, Cassandra, ScyllaDB to your HypGo project`,
	Args:  cobra.ExactArgs(1),
	RunE:  runInstall,
}

func runInstall(cmd *cobra.Command, args []string) error {
	pluginName := strings.ToLower(args[0])

	// 檢查是否在項目目錄中
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		return fmt.Errorf("please run this command in a HypGo project directory")
	}

	switch pluginName {
	case "rabbitmq":
		return addRabbitMQ()
	case "kafka":
		return addKafka()
	case "cassandra":
		return addCassandra()
	case "scylladb":
		return addScyllaDB()
	case "mongodb":
		return addMongoDB()
	case "elasticsearch":
		return addElasticsearch()
	default:
		return fmt.Errorf("unknown plugin: %s\nAvailable plugins: rabbitmq, kafka, cassandra, scylladb, mongodb, elasticsearch", pluginName)
	}
}

func addRabbitMQ() error {
	fmt.Println("📦 Adding RabbitMQ plugin...")

	// 創建 RabbitMQ 配置文件
	configContent := `# RabbitMQ Configuration
rabbitmq:
  url: "amqp://guest:guest@localhost:5672/"
  exchange: "hypgo"
  queue: "default"
  consumer:
    auto_ack: false
    exclusive: false
    no_local: false
    no_wait: false
  publisher:
    mandatory: false
    immediate: false
  qos:
    prefetch_count: 1
    prefetch_size: 0
    global: false
`

	if err := createConfigFile("rabbitmq.yaml", configContent); err != nil {
		return err
	}

	// 創建 RabbitMQ 服務文件
	serviceContent := `package rabbitmq

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"

    "github.com/streadway/amqp"
    "github.com/maoxiaoyue/hypgo/pkg/config"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type Service struct {
    config     *Config
    conn       *amqp.Connection
    channel    *amqp.Channel
    logger     *logger.Logger
    mu         sync.RWMutex
    consumers  map[string]context.CancelFunc
}

type Config struct {
    URL      string          ` + "`mapstructure:\"url\"`" + `
    Exchange string          ` + "`mapstructure:\"exchange\"`" + `
    Queue    string          ` + "`mapstructure:\"queue\"`" + `
    Consumer ConsumerConfig  ` + "`mapstructure:\"consumer\"`" + `
    Publisher PublisherConfig ` + "`mapstructure:\"publisher\"`" + `
    QoS      QoSConfig      ` + "`mapstructure:\"qos\"`" + `
}

type ConsumerConfig struct {
    AutoAck   bool ` + "`mapstructure:\"auto_ack\"`" + `
    Exclusive bool ` + "`mapstructure:\"exclusive\"`" + `
    NoLocal   bool ` + "`mapstructure:\"no_local\"`" + `
    NoWait    bool ` + "`mapstructure:\"no_wait\"`" + `
}

type PublisherConfig struct {
    Mandatory bool ` + "`mapstructure:\"mandatory\"`" + `
    Immediate bool ` + "`mapstructure:\"immediate\"`" + `
}

type QoSConfig struct {
    PrefetchCount int  ` + "`mapstructure:\"prefetch_count\"`" + `
    PrefetchSize  int  ` + "`mapstructure:\"prefetch_size\"`" + `
    Global        bool ` + "`mapstructure:\"global\"`" + `
}

func New(cfg *Config, log *logger.Logger) (*Service, error) {
    conn, err := amqp.Dial(cfg.URL)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
    }

    ch, err := conn.Channel()
    if err != nil {
        conn.Close()
        return nil, fmt.Errorf("failed to open channel: %w", err)
    }

    // 設置 QoS
    err = ch.Qos(
        cfg.QoS.PrefetchCount,
        cfg.QoS.PrefetchSize,
        cfg.QoS.Global,
    )
    if err != nil {
        ch.Close()
        conn.Close()
        return nil, fmt.Errorf("failed to set QoS: %w", err)
    }

    // 聲明交換機
    err = ch.ExchangeDeclare(
        cfg.Exchange,
        "topic",
        true,
        false,
        false,
        false,
        nil,
    )
    if err != nil {
        ch.Close()
        conn.Close()
        return nil, fmt.Errorf("failed to declare exchange: %w", err)
    }

    return &Service{
        config:    cfg,
        conn:      conn,
        channel:   ch,
        logger:    log,
        consumers: make(map[string]context.CancelFunc),
    }, nil
}

// 其他 RabbitMQ 方法...
`

	if err := createServiceFile("rabbitmq", serviceContent); err != nil {
		return err
	}

	// 更新 go.mod
	if err := updateGoMod("github.com/streadway/amqp", "v1.1.0"); err != nil {
		return err
	}

	fmt.Println("✅ RabbitMQ plugin added successfully!")
	fmt.Println("📄 Created config/rabbitmq.yaml")
	fmt.Println("📄 Created app/plugins/rabbitmq/service.go")
	fmt.Println("\n🔧 Next steps:")
	fmt.Println("1. Update config/rabbitmq.yaml with your RabbitMQ settings")
	fmt.Println("2. Import and use RabbitMQ in your controllers")

	return nil
}

func addKafka() error {
	fmt.Println("📦 Adding Kafka plugin...")

	configContent := `# Kafka Configuration
kafka:
  brokers:
    - "localhost:9092"
  consumer:
    group_id: "hypgo-consumer"
    auto_offset_reset: "earliest"
    enable_auto_commit: true
    auto_commit_interval: 1000
  producer:
    required_acks: 1
    compression_type: "none"
    max_message_bytes: 1000000
  topics:
    default: "hypgo-topic"
  sasl:
    enabled: false
    mechanism: "PLAIN"
    username: ""
    password: ""
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
`

	if err := createConfigFile("kafka.yaml", configContent); err != nil {
		return err
	}

	serviceContent := `package kafka

import (
    "context"
    "fmt"
    "sync"

    "github.com/segmentio/kafka-go"
    "github.com/segmentio/kafka-go/sasl/plain"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type Service struct {
    config    *Config
    writer    *kafka.Writer
    readers   map[string]*kafka.Reader
    logger    *logger.Logger
    mu        sync.RWMutex
}

type Config struct {
    Brokers  []string        ` + "`mapstructure:\"brokers\"`" + `
    Consumer ConsumerConfig  ` + "`mapstructure:\"consumer\"`" + `
    Producer ProducerConfig  ` + "`mapstructure:\"producer\"`" + `
    Topics   map[string]string ` + "`mapstructure:\"topics\"`" + `
    SASL     SASLConfig      ` + "`mapstructure:\"sasl\"`" + `
    TLS      TLSConfig       ` + "`mapstructure:\"tls\"`" + `
}

type ConsumerConfig struct {
    GroupID            string ` + "`mapstructure:\"group_id\"`" + `
    AutoOffsetReset    string ` + "`mapstructure:\"auto_offset_reset\"`" + `
    EnableAutoCommit   bool   ` + "`mapstructure:\"enable_auto_commit\"`" + `
    AutoCommitInterval int    ` + "`mapstructure:\"auto_commit_interval\"`" + `
}

type ProducerConfig struct {
    RequiredAcks     int    ` + "`mapstructure:\"required_acks\"`" + `
    CompressionType  string ` + "`mapstructure:\"compression_type\"`" + `
    MaxMessageBytes  int    ` + "`mapstructure:\"max_message_bytes\"`" + `
}

type SASLConfig struct {
    Enabled   bool   ` + "`mapstructure:\"enabled\"`" + `
    Mechanism string ` + "`mapstructure:\"mechanism\"`" + `
    Username  string ` + "`mapstructure:\"username\"`" + `
    Password  string ` + "`mapstructure:\"password\"`" + `
}

type TLSConfig struct {
    Enabled  bool   ` + "`mapstructure:\"enabled\"`" + `
    CertFile string ` + "`mapstructure:\"cert_file\"`" + `
    KeyFile  string ` + "`mapstructure:\"key_file\"`" + `
    CAFile   string ` + "`mapstructure:\"ca_file\"`" + `
}

func New(cfg *Config, log *logger.Logger) (*Service, error) {
    writerConfig := kafka.WriterConfig{
        Brokers:  cfg.Brokers,
        Topic:    cfg.Topics["default"],
        Balancer: &kafka.LeastBytes{},
    }

    if cfg.SASL.Enabled {
        mechanism := plain.Mechanism{
            Username: cfg.SASL.Username,
            Password: cfg.SASL.Password,
        }
        writerConfig.Dialer = &kafka.Dialer{
            SASLMechanism: mechanism,
        }
    }

    writer := kafka.NewWriter(writerConfig)

    return &Service{
        config:  cfg,
        writer:  writer,
        readers: make(map[string]*kafka.Reader),
        logger:  log,
    }, nil
}

// 其他 Kafka 方法...
`

	if err := createServiceFile("kafka", serviceContent); err != nil {
		return err
	}

	if err := updateGoMod("github.com/segmentio/kafka-go", "v0.4.47"); err != nil {
		return err
	}

	fmt.Println("✅ Kafka plugin added successfully!")
	fmt.Println("📄 Created config/kafka.yaml")
	fmt.Println("📄 Created app/plugins/kafka/service.go")

	return nil
}

func addCassandra() error {
	fmt.Println("📦 Adding Cassandra plugin...")

	configContent := `# Cassandra Configuration
cassandra:
  hosts:
    - "localhost:9042"
  keyspace: "hypgo"
  consistency: "QUORUM"
  proto_version: 4
  connect_timeout: 10s
  timeout: 10s
  num_conns: 2
  auth:
    enabled: false
    username: ""
    password: ""
  ssl:
    enabled: false
    cert_file: ""
    key_file: ""
    ca_file: ""
`

	if err := createConfigFile("cassandra.yaml", configContent); err != nil {
		return err
	}

	serviceContent := `package cassandra

import (
    "fmt"
    "strings"
    "time"

    "github.com/gocql/gocql"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type Service struct {
    config  *Config
    session *gocql.Session
    logger  *logger.Logger
}

type Config struct {
    Hosts          []string      ` + "`mapstructure:\"hosts\"`" + `
    Keyspace       string        ` + "`mapstructure:\"keyspace\"`" + `
    Consistency    string        ` + "`mapstructure:\"consistency\"`" + `
    ProtoVersion   int           ` + "`mapstructure:\"proto_version\"`" + `
    ConnectTimeout time.Duration ` + "`mapstructure:\"connect_timeout\"`" + `
    Timeout        time.Duration ` + "`mapstructure:\"timeout\"`" + `
    NumConns       int           ` + "`mapstructure:\"num_conns\"`" + `
    Auth           AuthConfig    ` + "`mapstructure:\"auth\"`" + `
    SSL            SSLConfig     ` + "`mapstructure:\"ssl\"`" + `
}

type AuthConfig struct {
    Enabled  bool   ` + "`mapstructure:\"enabled\"`" + `
    Username string ` + "`mapstructure:\"username\"`" + `
    Password string ` + "`mapstructure:\"password\"`" + `
}

type SSLConfig struct {
    Enabled  bool   ` + "`mapstructure:\"enabled\"`" + `
    CertFile string ` + "`mapstructure:\"cert_file\"`" + `
    KeyFile  string ` + "`mapstructure:\"key_file\"`" + `
    CAFile   string ` + "`mapstructure:\"ca_file\"`" + `
}

func New(cfg *Config, log *logger.Logger) (*Service, error) {
    cluster := gocql.NewCluster(cfg.Hosts...)
    cluster.Keyspace = cfg.Keyspace
    cluster.ProtoVersion = cfg.ProtoVersion
    cluster.ConnectTimeout = cfg.ConnectTimeout
    cluster.Timeout = cfg.Timeout
    cluster.NumConns = cfg.NumConns

    // 設置一致性級別
    switch strings.ToUpper(cfg.Consistency) {
    case "ANY":
        cluster.Consistency = gocql.Any
    case "ONE":
        cluster.Consistency = gocql.One
    case "TWO":
        cluster.Consistency = gocql.Two
    case "THREE":
        cluster.Consistency = gocql.Three
    case "QUORUM":
        cluster.Consistency = gocql.Quorum
    case "ALL":
        cluster.Consistency = gocql.All
    default:
        cluster.Consistency = gocql.Quorum
    }

    // 設置認證
    if cfg.Auth.Enabled {
        cluster.Authenticator = gocql.PasswordAuthenticator{
            Username: cfg.Auth.Username,
            Password: cfg.Auth.Password,
        }
    }

    session, err := cluster.CreateSession()
    if err != nil {
        return nil, fmt.Errorf("failed to create cassandra session: %w", err)
    }

    return &Service{
        config:  cfg,
        session: session,
        logger:  log,
    }, nil
}

// 其他 Cassandra 方法...
`

	if err := createServiceFile("cassandra", serviceContent); err != nil {
		return err
	}

	if err := updateGoMod("github.com/gocql/gocql", "v1.6.0"); err != nil {
		return err
	}

	fmt.Println("✅ Cassandra plugin added successfully!")
	fmt.Println("📄 Created config/cassandra.yaml")
	fmt.Println("📄 Created app/plugins/cassandra/service.go")

	return nil
}

func addScyllaDB() error {
	fmt.Println("📦 Adding ScyllaDB plugin...")

	// ScyllaDB 使用與 Cassandra 相同的驅動，但配置略有不同
	configContent := `# ScyllaDB Configuration
scylladb:
  hosts:
    - "localhost:9042"
  keyspace: "hypgo"
  consistency: "LOCAL_QUORUM"
  proto_version: 4
  connect_timeout: 5s
  timeout: 5s
  num_conns: 4
  pool_size: 4
  page_size: 5000
  auth:
    enabled: false
    username: ""
    password: ""
  compression: "snappy"
  retry_policy:
    num_retries: 3
  host_selection_policy: "token_aware"
`

	if err := createConfigFile("scylladb.yaml", configContent); err != nil {
		return err
	}

	serviceContent := `package scylladb

import (
    "fmt"
    "strings"
    "time"

    "github.com/gocql/gocql"
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

type Service struct {
    config  *Config
    session *gocql.Session
    logger  *logger.Logger
}

type Config struct {
    Hosts                []string         ` + "`mapstructure:\"hosts\"`" + `
    Keyspace             string           ` + "`mapstructure:\"keyspace\"`" + `
    Consistency          string           ` + "`mapstructure:\"consistency\"`" + `
    ProtoVersion         int              ` + "`mapstructure:\"proto_version\"`" + `
    ConnectTimeout       time.Duration    ` + "`mapstructure:\"connect_timeout\"`" + `
    Timeout              time.Duration    ` + "`mapstructure:\"timeout\"`" + `
    NumConns             int              ` + "`mapstructure:\"num_conns\"`" + `
    PoolSize             int              ` + "`mapstructure:\"pool_size\"`" + `
    PageSize             int              ` + "`mapstructure:\"page_size\"`" + `
    Auth                 AuthConfig       ` + "`mapstructure:\"auth\"`" + `
    Compression          string           ` + "`mapstructure:\"compression\"`" + `
    RetryPolicy          RetryPolicyConfig ` + "`mapstructure:\"retry_policy\"`" + `
    HostSelectionPolicy  string           ` + "`mapstructure:\"host_selection_policy\"`" + `
}

type AuthConfig struct {
    Enabled  bool   ` + "`mapstructure:\"enabled\"`" + `
    Username string ` + "`mapstructure:\"username\"`" + `
    Password string ` + "`mapstructure:\"password\"`" + `
}

type RetryPolicyConfig struct {
    NumRetries int ` + "`mapstructure:\"num_retries\"`" + `
}

func New(cfg *Config, log *logger.Logger) (*Service, error) {
    cluster := gocql.NewCluster(cfg.Hosts...)
    cluster.Keyspace = cfg.Keyspace
    cluster.ProtoVersion = cfg.ProtoVersion
    cluster.ConnectTimeout = cfg.ConnectTimeout
    cluster.Timeout = cfg.Timeout
    cluster.NumConns = cfg.NumConns
    cluster.PageSize = cfg.PageSize

    // ScyllaDB 優化設置
    cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())
    
    // 設置壓縮
    switch strings.ToLower(cfg.Compression) {
    case "snappy":
        cluster.Compressor = gocql.SnappyCompressor{}
    case "lz4":
        cluster.Compressor = &gocql.LZ4Compressor{}
    }

    // 設置重試策略
    cluster.RetryPolicy = &gocql.SimpleRetryPolicy{
        NumRetries: cfg.RetryPolicy.NumRetries,
    }

    session, err := cluster.CreateSession()
    if err != nil {
        return nil, fmt.Errorf("failed to create scylladb session: %w", err)
    }

    return &Service{
        config:  cfg,
        session: session,
        logger:  log,
    }, nil
}

// 其他 ScyllaDB 方法...
`

	if err := createServiceFile("scylladb", serviceContent); err != nil {
		return err
	}

	if err := updateGoMod("github.com/gocql/gocql", "v1.6.0"); err != nil {
		return err
	}

	fmt.Println("✅ ScyllaDB plugin added successfully!")
	fmt.Println("📄 Created config/scylladb.yaml")
	fmt.Println("📄 Created app/plugins/scylladb/service.go")

	return nil
}

func addMongoDB() error {
	fmt.Println("📦 Adding MongoDB plugin...")

	configContent := `# MongoDB Configuration
mongodb:
  uri: "mongodb://localhost:27017"
  database: "hypgo"
  auth:
    enabled: false
    username: ""
    password: ""
    auth_source: "admin"
  connection:
    max_pool_size: 100
    min_pool_size: 10
    max_idle_time: 10m
    timeout: 10s
  read_preference: "primary"
  write_concern:
    w: "majority"
    j: true
    timeout: 5s
`

	if err := createConfigFile("mongodb.yaml", configContent); err != nil {
		return err
	}

	if err := updateGoMod("go.mongodb.org/mongo-driver", "v1.13.1"); err != nil {
		return err
	}

	fmt.Println("✅ MongoDB plugin added successfully!")
	fmt.Println("📄 Created config/mongodb.yaml")

	return nil
}

func addElasticsearch() error {
	fmt.Println("📦 Adding Elasticsearch plugin...")

	configContent := `# Elasticsearch Configuration
elasticsearch:
  addresses:
    - "http://localhost:9200"
  username: ""
  password: ""
  cloud_id: ""
  api_key: ""
  index:
    default: "hypgo"
    number_of_shards: 1
    number_of_replicas: 0
  retry:
    max_retries: 3
    backoff:
      initial: 100ms
      max: 1s
  transport:
    max_idle_conns: 10
    max_idle_conns_per_host: 2
    timeout: 10s
`

	if err := createConfigFile("elasticsearch.yaml", configContent); err != nil {
		return err
	}

	if err := updateGoMod("github.com/elastic/go-elasticsearch/v8", "v8.11.0"); err != nil {
		return err
	}

	fmt.Println("✅ Elasticsearch plugin added successfully!")
	fmt.Println("📄 Created config/elasticsearch.yaml")

	return nil
}

func createConfigFile(filename, content string) error {
	configPath := filepath.Join("config", filename)

	// 確保 config 目錄存在
	if err := os.MkdirAll("config", 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 檢查文件是否已存在
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("⚠️  %s already exists, skipping...\n", configPath)
		return nil
	}

	return os.WriteFile(configPath, []byte(content), 0644)
}

func createServiceFile(pluginName, content string) error {
	pluginPath := filepath.Join("app", "plugins", pluginName)

	// 創建插件目錄
	if err := os.MkdirAll(pluginPath, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	servicePath := filepath.Join(pluginPath, "service.go")

	// 檢查文件是否已存在
	if _, err := os.Stat(servicePath); err == nil {
		fmt.Printf("⚠️  %s already exists, skipping...\n", servicePath)
		return nil
	}

	return os.WriteFile(servicePath, []byte(content), 0644)
}

func updateGoMod(module, version string) error {
	// 這裡簡化處理，實際應該解析 go.mod 文件
	fmt.Printf("📦 Adding dependency: %s %s\n", module, version)
	fmt.Println("   Run 'go mod tidy' to download dependencies")
	return nil
}
