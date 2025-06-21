package config

import (
	"encoding/json"
	"fmt"
	"github.com/miaoxiaoyue/hypgo/pkg/logger"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logger   LoggerConfig   `mapstructure:"logger"`
	RabbitMQ RabbitMQConfig `mapstructure:"rabbitmq"`
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

type RabbitMQ struct {
	config    *config.RabbitMQConfig
	conn      *amqp.Connection
	channel   *amqp.Channel
	logger    *logger.Logger
	mu        sync.RWMutex
	consumers map[string]context.CancelFunc
}

func NewRabbitMQ(cfg *config.RabbitMQConfig, log *logger.Logger) (*RabbitMQ, error) {
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to open channel: %w", err)
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

	return &RabbitMQ{
		config:    cfg,
		conn:      conn,
		channel:   ch,
		logger:    log,
		consumers: make(map[string]context.CancelFunc),
	}, nil
}

func (r *RabbitMQ) Publish(routingKey string, message interface{}) error {
	body, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	err = r.channel.Publish(
		r.config.Exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
			Timestamp:   time.Now(),
		},
	)

	if err != nil {
		r.logger.Warning("Failed to publish message: %v", err)
		return fmt.Errorf("failed to publish message: %w", err)
	}

	r.logger.Debug("Published message to %s with routing key %s", r.config.Exchange, routingKey)
	return nil
}

func (r *RabbitMQ) Subscribe(queueName, routingKey string, handler func([]byte) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 聲明隊列
	q, err := r.channel.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// 綁定隊列到交換機
	err = r.channel.QueueBind(
		q.Name,
		routingKey,
		r.config.Exchange,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}

	// 開始消費
	msgs, err := r.channel.Consume(
		q.Name,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register consumer: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.consumers[queueName] = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				r.logger.Info("Consumer %s stopped", queueName)
				return
			case msg, ok := <-msgs:
				if !ok {
					r.logger.Warning("Channel closed for consumer %s", queueName)
					return
				}

				if err := handler(msg.Body); err != nil {
					r.logger.Warning("Handler error: %v", err)
					msg.Nack(false, true)
				} else {
					msg.Ack(false)
				}
			}
		}
	}()

	r.logger.Info("Started consumer for queue %s with routing key %s", queueName, routingKey)
	return nil
}

func (r *RabbitMQ) Unsubscribe(queueName string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cancel, ok := r.consumers[queueName]; ok {
		cancel()
		delete(r.consumers, queueName)
		r.logger.Info("Unsubscribed from queue %s", queueName)
	}
}

func (r *RabbitMQ) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 停止所有消費者
	for _, cancel := range r.consumers {
		cancel()
	}

	if r.channel != nil {
		r.channel.Close()
	}

	if r.conn != nil {
		return r.conn.Close()
	}

	return nil
}

// 確保連接健康
func (r *RabbitMQ) HealthCheck() error {
	if r.conn.IsClosed() {
		return fmt.Errorf("connection is closed")
	}
	return nil
}

// 重新連接
func (r *RabbitMQ) Reconnect() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn != nil && !r.conn.IsClosed() {
		return nil
	}

	conn, err := amqp.Dial(r.config.URL)
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to reopen channel: %w", err)
	}

	r.conn = conn
	r.channel = ch

	r.logger.Info("Successfully reconnected to RabbitMQ")
	return nil
}
