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
	// 插件配置緩存
	pluginConfigs map[string]map[string]interface{}
}

// NewManager 創建插件管理器
func NewManager(cfg *config.Config, log *logger.Logger) *Manager {
	if log == nil {
		log = logger.NewLogger()
	}

	return &Manager{
		plugins:       make(map[string]Plugin),
		config:        cfg,
		logger:        log,
		pluginConfigs: make(map[string]map[string]interface{}),
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
	m.logger.Info("Plugin registered", "name", name)
	return nil
}

// InitAll 初始化所有插件
func (m *Manager) InitAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for name, plugin := range m.plugins {
		pluginConfig, ok := m.GetPluginConfig(name)
		if !ok {
			m.logger.Warn("No configuration found for plugin, using defaults", "plugin", name)
			pluginConfig = make(map[string]interface{})
		}

		if err := plugin.Init(pluginConfig, m.logger); err != nil {
			return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
		}
		m.logger.Info("Plugin initialized", "name", name)
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
		m.logger.Info("Plugin started", "name", name)
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
			m.logger.Warn("Failed to stop plugin", "name", name, "error", err)
		} else {
			m.logger.Info("Plugin stopped", "name", name)
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

// List 列出所有已註冊的插件
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

// GetPluginConfig 獲取插件配置
func (m *Manager) GetPluginConfig(name string) (map[string]interface{}, bool) {
	// 先檢查緩存
	if config, ok := m.pluginConfigs[name]; ok {
		return config, true
	}

	// 從主配置中獲取
	if m.config == nil {
		return nil, false
	}

	// 根據插件名稱獲取對應的配置
	var pluginConfig map[string]interface{}

	switch name {
	case "cassandra":
		pluginConfig = m.cassandraConfigToMap(m.config.Cassandra)
	case "benchmark":
		// 從配置文件讀取 benchmark 配置
		pluginConfig = m.loadPluginConfigFromFile("benchmark")
	case "elasticsearch":
		pluginConfig = m.loadPluginConfigFromFile("elasticsearch")
	case "kafka":
		pluginConfig = m.loadPluginConfigFromFile("kafka")
	case "rabbitmq":
		pluginConfig = m.loadPluginConfigFromFile("rabbitmq")
	case "redis":
		pluginConfig = m.loadPluginConfigFromFile("redis")
	case "mongodb":
		pluginConfig = m.loadPluginConfigFromFile("mongodb")
	case "mysql":
		pluginConfig = m.loadPluginConfigFromFile("mysql")
	case "postgresql":
		pluginConfig = m.loadPluginConfigFromFile("postgresql")
	case "nats":
		pluginConfig = m.loadPluginConfigFromFile("nats")
	case "zeromq":
		pluginConfig = m.loadPluginConfigFromFile("zeromq")
	default:
		// 嘗試從文件加載
		pluginConfig = m.loadPluginConfigFromFile(name)
	}

	if pluginConfig != nil {
		// 緩存配置
		m.pluginConfigs[name] = pluginConfig
		return pluginConfig, true
	}

	return nil, false
}

// SetPluginConfig 設置插件配置
func (m *Manager) SetPluginConfig(name string, config map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pluginConfigs[name] = config
}

// loadPluginConfigFromFile 從文件加載插件配置
func (m *Manager) loadPluginConfigFromFile(pluginName string) map[string]interface{} {
	// 這裡簡化處理，實際應該從 config/<plugin>.yaml 文件讀取
	// 可以使用 viper 或其他配置庫來實現

	// 返回預設配置
	defaultConfigs := map[string]map[string]interface{}{
		"benchmark": {
			"enabled":            true,
			"output_dir":         "benchmark_results",
			"default_iterations": 1000,
			"warmup_runs":        10,
			"timeout":            "30s",
			"collect_mem_stats":  true,
			"collect_io_stats":   true,
		},
		"elasticsearch": {
			"enabled":   true,
			"addresses": []string{"http://localhost:9200"},
			"username":  "",
			"password":  "",
		},
		"kafka": {
			"enabled":  true,
			"brokers":  []string{"localhost:9092"},
			"group_id": "hypgo-consumer",
		},
		"rabbitmq": {
			"enabled":  true,
			"host":     "localhost",
			"port":     5672,
			"username": "guest",
			"password": "guest",
		},
		"redis": {
			"enabled":  true,
			"host":     "localhost",
			"port":     6379,
			"password": "",
			"database": 0,
		},
		"mongodb": {
			"enabled":  true,
			"uri":      "mongodb://localhost:27017",
			"database": "hypgo",
		},
		"mysql": {
			"enabled":  true,
			"host":     "localhost",
			"port":     3306,
			"database": "hypgo",
			"username": "root",
			"password": "",
		},
		"postgresql": {
			"enabled":  true,
			"host":     "localhost",
			"port":     5432,
			"database": "hypgo",
			"username": "postgres",
			"password": "",
		},
	}

	if config, ok := defaultConfigs[pluginName]; ok {
		return config
	}

	return nil
}

// cassandraConfigToMap 將 Cassandra 配置轉換為 map
func (m *Manager) cassandraConfigToMap(cfg config.CassandraConfig) map[string]interface{} {
	return map[string]interface{}{
		"enabled":            true,
		"hosts":              cfg.Hosts,
		"keyspace":           cfg.Keyspace,
		"username":           cfg.Username,
		"password":           cfg.Password,
		"consistency":        cfg.Consistency,
		"compression":        cfg.Compression,
		"connect_timeout":    cfg.ConnectTimeout,
		"timeout":            cfg.Timeout,
		"num_conns":          cfg.NumConns,
		"max_conns":          cfg.MaxConns,
		"max_retries":        cfg.MaxRetries,
		"enable_logging":     cfg.EnableLogging,
		"page_size":          cfg.PageSize,
		"shard_aware_port":   cfg.ShardAwarePort,
		"enable_shard_aware": cfg.EnableShardAware,
	}
}

// AutoRegister 自動註冊已安裝的插件
func (m *Manager) AutoRegister() {
	// 檢查並註冊各種插件
	pluginNames := []string{
		"cassandra", "scylladb", "elasticsearch", "kafka",
		"rabbitmq", "redis", "mongodb", "mysql", "postgresql",
		"nats", "zeromq", "benchmark",
	}

	for _, name := range pluginNames {
		if config, ok := m.GetPluginConfig(name); ok {
			if enabled, ok := config["enabled"].(bool); ok && enabled {
				m.logger.Info("Found plugin configuration", "plugin", name)
				// 這裡實際應該導入並註冊具體的插件實現
				// 例如: m.Register(NewPluginInstance(name))
			}
		}
	}
}

// Reload 重新載入插件配置
func (m *Manager) Reload() error {
	m.logger.Info("Reloading plugin configurations...")

	// 停止所有插件
	if err := m.StopAll(); err != nil {
		return fmt.Errorf("failed to stop plugins: %w", err)
	}

	// 清除配置緩存
	m.mu.Lock()
	m.pluginConfigs = make(map[string]map[string]interface{})
	m.mu.Unlock()

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

// UpdateConfig 更新插件管理器的主配置
func (m *Manager) UpdateConfig(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = cfg
	// 清除配置緩存，下次獲取時會重新加載
	m.pluginConfigs = make(map[string]map[string]interface{})
}
