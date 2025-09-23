package scylladb

import (
	"github.com/maoxiaoyue/hypgo/pkg/database"
	"github.com/maoxiaoyue/hypgo/pkg/plugins/cassandra"
)

// ScyllaDBPlugin ScyllaDB 數據庫插件（基於 Cassandra 插件）
type ScyllaDBPlugin struct {
	*cassandra.CassandraPlugin
}

// NewScyllaDBPlugin 創建 ScyllaDB 插件
func NewScyllaDBPlugin() database.DatabasePlugin {
	return &ScyllaDBPlugin{
		CassandraPlugin: &cassandra.CassandraPlugin{},
	}
}

// Name 插件名稱
func (s *ScyllaDBPlugin) Name() string {
	return "scylladb"
}

// Init 初始化插件配置（優化 ScyllaDB 特定設置）
func (s *ScyllaDBPlugin) Init(configMap map[string]interface{}) error {
	// 先調用父類的初始化
	if err := s.CassandraPlugin.Init(configMap); err != nil {
		return err
	}

	// ScyllaDB 特定優化
	// 默認啟用 Shard-aware
	if _, exists := configMap["enable_shard_aware"]; !exists {
		configMap["enable_shard_aware"] = true
	}

	// 默認使用更高的連接數
	if _, exists := configMap["num_conns"]; !exists {
		configMap["num_conns"] = 4
	}

	// 默認使用 LZ4 壓縮
	if _, exists := configMap["compression"]; !exists {
		configMap["compression"] = "lz4"
	}

	return nil
}

// 註冊插件到數據庫管理器的輔助函數
func Register(db *database.Database) error {
	plugin := NewScyllaDBPlugin()
	return db.RegisterPlugin(plugin)
}
