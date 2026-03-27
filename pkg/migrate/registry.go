// Package migrate 提供 Migration Diff 功能
// 掃描 Go Model struct，比對快照，自動產生 SQL migration
package migrate

// ModelRegistry 儲存所有需要管理的 Model
type ModelRegistry struct {
	models []interface{}
}

// NewRegistry 建立新的 ModelRegistry
func NewRegistry() *ModelRegistry {
	return &ModelRegistry{
		models: make([]interface{}, 0),
	}
}

// Register 註冊 Model（傳入 nil pointer）
//
// 使用範例：
//
//	registry.Register(
//	    (*models.User)(nil),
//	    (*models.Post)(nil),
//	)
func (r *ModelRegistry) Register(models ...interface{}) {
	r.models = append(r.models, models...)
}

// Models 返回所有已註冊的 Model
func (r *ModelRegistry) Models() []interface{} {
	return r.models
}

// Len 返回已註冊數量
func (r *ModelRegistry) Len() int {
	return len(r.models)
}
