package schema

import (
	"sync"
)

// Registry 儲存所有 schema-registered 路由的全域註冊表
// 執行緒安全，使用 sync.RWMutex 保護
// 支援多協議：REST、gRPC、Bot、MCP、WebSocket、CLI
type Registry struct {
	mu      sync.RWMutex
	schemas []Route
	byKey   map[string]*Route // key = RouteKey()
}

var globalRegistry = &Registry{
	schemas: make([]Route, 0),
	byKey:   make(map[string]*Route),
}

// Global 返回全域 schema 註冊表
func Global() *Registry {
	return globalRegistry
}

// Register 註冊一個路由 schema（支援所有協議）
func (r *Registry) Register(route Route) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := route.RouteKey()
	if existing, ok := r.byKey[key]; ok {
		*existing = route
		return
	}

	r.schemas = append(r.schemas, route)
	r.byKey[key] = &r.schemas[len(r.schemas)-1]
}

// Get 根據 method 和 path 查詢 REST schema（向後相容）
func (r *Registry) Get(method, path string) (*Route, bool) {
	return r.GetByKey("rest|" + method + " " + path)
}

// GetByKey 根據完整 key 查詢 schema
func (r *Registry) GetByKey(key string) (*Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	route, ok := r.byKey[key]
	return route, ok
}

// GetByProtocol 回傳指定協議的所有 schema
func (r *Registry) GetByProtocol(protocol string) []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Route
	for _, route := range r.schemas {
		p := route.Protocol
		if p == "" {
			p = "rest"
		}
		if p == protocol {
			result = append(result, route)
		}
	}
	return result
}

// All 返回所有已註冊的路由 schema（副本）
func (r *Registry) All() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Route, len(r.schemas))
	copy(result, r.schemas)
	return result
}

// Len 返回已註冊的 schema 數量
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.schemas)
}

// Reset 清空所有已註冊的 schema（用於測試）
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.schemas = make([]Route, 0)
	r.byKey = make(map[string]*Route)
}
