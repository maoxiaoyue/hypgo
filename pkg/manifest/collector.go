package manifest

import (
	"reflect"
	"sort"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/migrate"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// Collector 從框架各元件收集資訊，組裝成 Manifest
type Collector struct {
	router          *router.Router
	config          *config.Config
	registry        *schema.Registry
	migrateRegistry *migrate.ModelRegistry
	enrichCfg       EnrichConfig
}

// NewCollector 建立新的 Collector（使用預設推斷配置）
// config 可為 nil（不含伺服器資訊）
func NewCollector(r *router.Router, cfg *config.Config) *Collector {
	return &Collector{
		router:    r,
		config:    cfg,
		registry:  schema.Global(),
		enrichCfg: DefaultEnrichConfig(),
	}
}

// NewCollectorWithEnrich 建立帶自訂推斷配置的 Collector
func NewCollectorWithEnrich(r *router.Router, cfg *config.Config, enrichCfg EnrichConfig) *Collector {
	return &Collector{
		router:    r,
		config:    cfg,
		registry:  schema.Global(),
		enrichCfg: enrichCfg,
	}
}

// NewCollectorWithModels 建立帶 Model 描述的 Collector
// registry 為 nil 時不收集 model 資訊
func NewCollectorWithModels(r *router.Router, cfg *config.Config, registry *migrate.ModelRegistry) *Collector {
	return &Collector{
		router:          r,
		config:          cfg,
		registry:        schema.Global(),
		migrateRegistry: registry,
		enrichCfg:       DefaultEnrichConfig(),
	}
}

// NewCollectorWithLLM 建立帶 LLM 增強的 Collector
// llmCfg 為 nil 或 mode=none 時退回純推斷模式
func NewCollectorWithLLM(r *router.Router, cfg *config.Config, llmCfg *config.LLMConfig) (*Collector, error) {
	enrichCfg := DefaultEnrichConfig()

	if llmCfg != nil && llmCfg.IsEnabled() {
		enricher, err := NewLLMEnricherFromConfig(llmCfg)
		if err != nil {
			return nil, err
		}
		enrichCfg.LLMEnricher = enricher
	}

	return &Collector{
		router:    r,
		config:    cfg,
		registry:  schema.Global(),
		enrichCfg: enrichCfg,
	}, nil
}

// Collect 組裝完整的 Manifest
func (c *Collector) Collect() *Manifest {
	m := &Manifest{
		Version:     "1.0",
		Framework:   "HypGo",
		GeneratedAt: time.Now(),
		Routes:      c.collectRoutes(),
		Models:      c.collectModels(),
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

// collectRoutes 從 Router 收集 REST 路由，並追加 Schema Registry 中的非 REST 路由
func (c *Collector) collectRoutes() []RouteManifest {
	manifests := make([]RouteManifest, 0)

	// 1. 收集 HTTP REST 路由（從 Router 的 Radix Tree）
	if c.router != nil {
		routeInfos := c.router.Routes()
		for _, ri := range routeInfos {
			rm := RouteManifest{
				Protocol:     "rest",
				Method:       ri.Method,
				Path:         ri.Path,
				HandlerNames: ri.HandlerNames,
			}

			// 用 schema metadata 豐富路由資訊
			var schemaRoute *schema.Route
			if s, ok := c.registry.Get(ri.Method, ri.Path); ok {
				schemaRoute = s
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

			// 智慧推斷回填空白欄位
			enrichRoute(&rm, schemaRoute, c.enrichCfg)

			manifests = append(manifests, rm)
		}
	}

	// 2. 收集非 REST 路由（直接從 Schema Registry，不經過 Router）
	for _, s := range c.registry.All() {
		if s.IsREST() {
			continue // REST 路由已在上面從 Router 收集
		}

		rm := RouteManifest{
			Protocol:    s.Protocol,
			Command:     s.Command,
			Platform:    s.Platform,
			Summary:     s.Summary,
			Description: s.Description,
			Tags:        s.Tags,
			InputType:   s.InputName,
			OutputType:  s.OutputName,
		}

		if len(s.Responses) > 0 {
			rm.Responses = make(map[int]string)
			for code, resp := range s.Responses {
				rm.Responses[code] = resp.Description
			}
		}

		// 智慧推斷回填空白欄位
		sCopy := s
		enrichRoute(&rm, &sCopy, c.enrichCfg)

		manifests = append(manifests, rm)
	}

	// 排序：REST 按 Path，非 REST 按 Protocol + Command
	sort.Slice(manifests, func(i, j int) bool {
		pi, pj := manifests[i].Protocol, manifests[j].Protocol
		if pi == "" {
			pi = "rest"
		}
		if pj == "" {
			pj = "rest"
		}
		if pi != pj {
			return pi < pj
		}
		// 同協議內排序
		if pi == "rest" {
			if manifests[i].Path != manifests[j].Path {
				return manifests[i].Path < manifests[j].Path
			}
			return manifests[i].Method < manifests[j].Method
		}
		return manifests[i].Command < manifests[j].Command
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

// collectModels 從 migrate.ModelRegistry 收集 Model 描述
// 直接掃描每個 model，同時取得 struct 名稱與 table schema
func (c *Collector) collectModels() []ModelManifest {
	if c.migrateRegistry == nil || c.migrateRegistry.Len() == 0 {
		return nil
	}

	models := c.migrateRegistry.Models()
	manifests := make([]ModelManifest, 0, len(models))

	for _, model := range models {
		table := migrate.ScanModel(model)
		if table == nil {
			continue
		}

		mm := ModelManifest{
			Name:   structName(model), // 使用原始 struct 名稱，如 "User"
			Table:  table.Name,        // bun tag 解析的表名，如 "users"
			Fields: make([]FieldManifest, 0, len(table.Columns)),
		}
		for _, col := range table.Columns {
			mm.Fields = append(mm.Fields, FieldManifest{
				Name:          col.Name,
				GoType:        col.GoType,
				SQLType:       col.SQLType,
				PrimaryKey:    col.PrimaryKey,
				AutoIncrement: col.AutoIncrement,
				NotNull:       col.NotNull,
				Unique:        col.Unique,
				Default:       col.Default,
			})
		}
		manifests = append(manifests, mm)
	}

	// 依表名排序，確保輸出穩定
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Table < manifests[j].Table
	})

	return manifests
}

// structName 取得 model 的原始 struct 名稱（去除 pointer 層）
func structName(model interface{}) string {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}
