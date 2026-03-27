package manifest

import (
	"sort"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// Collector 從框架各元件收集資訊，組裝成 Manifest
type Collector struct {
	router   *router.Router
	config   *config.Config
	registry *schema.Registry
}

// NewCollector 建立新的 Collector
// config 可為 nil（不含伺服器資訊）
func NewCollector(r *router.Router, cfg *config.Config) *Collector {
	return &Collector{
		router:   r,
		config:   cfg,
		registry: schema.Global(),
	}
}

// Collect 組裝完整的 Manifest
func (c *Collector) Collect() *Manifest {
	m := &Manifest{
		Version:     "1.0",
		Framework:   "HypGo",
		GeneratedAt: time.Now(),
		Routes:      c.collectRoutes(),
	}

	if c.config != nil {
		m.Server = c.collectServer()
		m.Database = c.collectDatabase()
	}

	return m
}

// collectServer 收集伺服器配置
func (c *Collector) collectServer() ServerInfo {
	return ServerInfo{
		Addr:     c.config.Server.Addr,
		Protocol: c.config.Server.Protocol,
		TLS:      c.config.Server.TLS.Enabled,
	}
}

// collectRoutes 從 Router 收集路由，並用 Schema Registry 豐富 metadata
func (c *Collector) collectRoutes() []RouteManifest {
	routeInfos := c.router.Routes()
	manifests := make([]RouteManifest, 0, len(routeInfos))

	for _, ri := range routeInfos {
		rm := RouteManifest{
			Method:       ri.Method,
			Path:         ri.Path,
			HandlerNames: ri.HandlerNames,
		}

		// 用 schema metadata 豐富路由資訊
		if s, ok := c.registry.Get(ri.Method, ri.Path); ok {
			rm.Summary = s.Summary
			rm.Description = s.Description
			rm.Tags = s.Tags
			rm.InputType = s.InputName
			rm.OutputType = s.OutputName

			if len(s.Responses) > 0 {
				rm.Responses = make(map[int]string)
				for code, resp := range s.Responses {
					rm.Responses[code] = resp.Description
				}
			}
		}

		manifests = append(manifests, rm)
	}

	// 按 Method + Path 排序
	sort.Slice(manifests, func(i, j int) bool {
		if manifests[i].Path != manifests[j].Path {
			return manifests[i].Path < manifests[j].Path
		}
		return manifests[i].Method < manifests[j].Method
	})

	return manifests
}

// collectDatabase 收集資料庫配置
func (c *Collector) collectDatabase() *DatabaseInfo {
	if c.config.Database.Driver == "" {
		return nil
	}
	return &DatabaseInfo{
		Driver:      c.config.Database.Driver,
		HasReplicas: len(c.config.Database.Replicas) > 0,
	}
}
