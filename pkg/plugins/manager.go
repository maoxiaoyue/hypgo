package plugins

import (
	"fmt"
	"sync"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// Plugin 插件接口
type Plugin interface {
	Name() string
	Init(config map[string]interface{}, logger *logger.Logger) error
	Start() error
	Stop() error
	Health() error
}

// Manager 插件管理器
type Manager struct {
	plugins map[string]Plugin
	config  *config.Config
	logger  *logger.Logger
	mu      sync.RWMutex
}

// NewManager 創建插件管理器
func NewManager(cfg *config.Config, log *logger.Logger) *Manager {
	return &Manager{
		plugins: make(map[string]Plugin),
		config:  cfg,
		logger:  log,
	}
}

// Register 註冊插件
func (m *Manager) Register(plugin Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := plugin.Name()
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	m.plugins[name] = plugin
	m.logger.Info("Plugin %s registered", name)
	return nil
}

// InitAll 初始化所有插件
func (m *Manager) InitAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, plugin := range m.plugins {
		pluginConfig, ok := m.config.GetPluginConfig(name)
		if !ok {
			m.logger.Warning("No configuration found for plugin %s, skipping", name)
			continue
		}

		if err := plugin.Init(pluginConfig, m.logger); err != nil {
			return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
		}

		m.logger.Info("Plugin %s initialized", name)
	}

	return nil
}

// StartAll 啟動所有插件
func (m *Manager) StartAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, plugin := range m.plugins {
		if err := plugin.Start(); err != nil {
			return fmt.Errorf("failed to start plugin %s: %w", name, err)
		}

		m.logger.Info("Plugin %s started", name)
	}

	return nil
}

// StopAll 停止所有插件
func (m *Manager) StopAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var errs []error

	for name, plugin := range m.plugins {
		if err := plugin.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("failed to stop plugin %s: %w", name, err))
			m.logger.Warning("Failed to stop plugin %s: %v", name, err)
		} else {
			m.logger.Info("Plugin %s stopped", name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors occurred while stopping plugins: %v", errs)
	}

	return nil
}

// Get 獲取插件
func (m *Manager) Get(name string) (Plugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, ok := m.plugins[name]
	return plugin, ok
}

// List 列出所有插件
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}

	return names
}

// HealthCheck 檢查所有插件健康狀態
func (m *Manager) HealthCheck() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)

	for name, plugin := range m.plugins {
		results[name] = plugin.Health()
	}

	return results
}

// AutoRegister 自動註冊已安裝的插件
func (m *Manager) AutoRegister() {
	// 檢查並註冊 RabbitMQ
	if _, ok := m.config.GetPluginConfig("rabbitmq"); ok {
		// 這裡需要導入具體的插件實現
		// m.Register(rabbitmq.NewPlugin())
		m.logger.Info("Found RabbitMQ configuration")
	}

	// 檢查並註冊 Kafka
	if _, ok := m.config.GetPluginConfig("kafka"); ok {
		// m.Register(kafka.NewPlugin())
		m.logger.Info("Found Kafka configuration")
	}

	// 檢查並註冊 Cassandra
	if _, ok := m.config.GetPluginConfig("cassandra"); ok {
		// m.Register(cassandra.NewPlugin())
		m.logger.Info("Found Cassandra configuration")
	}

	// 檢查並註冊 ScyllaDB
	if _, ok := m.config.GetPluginConfig("scylladb"); ok {
		// m.Register(scylladb.NewPlugin())
		m.logger.Info("Found ScyllaDB configuration")
	}

	// 檢查並註冊 MongoDB
	if _, ok := m.config.GetPluginConfig("mongodb"); ok {
		// m.Register(mongodb.NewPlugin())
		m.logger.Info("Found MongoDB configuration")
	}

	// 檢查並註冊 Elasticsearch
	if _, ok := m.config.GetPluginConfig("elasticsearch"); ok {
		// m.Register(elasticsearch.NewPlugin())
		m.logger.Info("Found Elasticsearch configuration")
	}
}

// Reload 重新載入插件配置
func (m *Manager) Reload() error {
	m.logger.Info("Reloading plugin configurations...")

	// 停止所有插件
	if err := m.StopAll(); err != nil {
		return fmt.Errorf("failed to stop plugins: %w", err)
	}

	// 重新初始化
	if err := m.InitAll(); err != nil {
		return fmt.Errorf("failed to reinitialize plugins: %w", err)
	}

	// 重新啟動
	if err := m.StartAll(); err != nil {
		return fmt.Errorf("failed to restart plugins: %w", err)
	}

	m.logger.Info("Plugin reload completed")
	return nil
}
