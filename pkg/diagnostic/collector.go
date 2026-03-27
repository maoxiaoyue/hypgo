package diagnostic

import (
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/router"
	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

var startTime = time.Now()

// DiagnosticState 完整的應用程式診斷狀態
type DiagnosticState struct {
	Timestamp   string         `json:"timestamp"`
	Uptime      string         `json:"uptime"`
	Runtime     RuntimeInfo    `json:"runtime"`
	Server      *ServerDiag    `json:"server,omitempty"`
	Routes      []RouteDiag    `json:"routes"`
	SchemaCount int            `json:"schema_count"`
	ErrorCount  int            `json:"error_count"`
	Database    *DatabaseDiag  `json:"database,omitempty"`
}

// RuntimeInfo Go runtime 資訊
type RuntimeInfo struct {
	GoVersion    string `json:"go_version"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	GOMAXPROCS   int    `json:"gomaxprocs"`
}

// ServerDiag 伺服器診斷
type ServerDiag struct {
	Addr     string `json:"addr"`
	Protocol string `json:"protocol"`
	TLS      bool   `json:"tls"`
}

// RouteDiag 路由診斷
type RouteDiag struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Handler string `json:"handler,omitempty"`
	Schema  bool   `json:"schema"`
}

// DatabaseDiag 資料庫診斷（安全遮蔽）
type DatabaseDiag struct {
	Driver      string `json:"driver"`
	Host        string `json:"host,omitempty"`
	HasReplicas bool   `json:"has_replicas"`
}

// Collect 收集診斷資訊
func Collect(r *router.Router, cfg *config.Config, redact bool) *DiagnosticState {
	state := &DiagnosticState{
		Timestamp: time.Now().Format(time.RFC3339),
		Uptime:    time.Since(startTime).Round(time.Second).String(),
		Runtime: RuntimeInfo{
			GoVersion:    runtime.Version(),
			GOOS:         runtime.GOOS,
			GOARCH:       runtime.GOARCH,
			NumCPU:       runtime.NumCPU(),
			NumGoroutine: runtime.NumGoroutine(),
			GOMAXPROCS:   runtime.GOMAXPROCS(0),
		},
		SchemaCount: schema.Global().Len(),
	}

	// 收集路由資訊
	state.Routes = collectRoutes(r)

	// 收集伺服器設定
	if cfg != nil {
		state.Server = &ServerDiag{
			Addr:     cfg.Server.Addr,
			Protocol: cfg.Server.Protocol,
			TLS:      cfg.Server.TLS.Enabled,
		}

		// 收集資料庫資訊（安全遮蔽）
		if cfg.Database.Driver != "" {
			state.Database = collectDatabase(cfg, redact)
		}
	}

	return state
}

// collectRoutes 收集路由診斷
func collectRoutes(r *router.Router) []RouteDiag {
	routes := r.Routes()
	diags := make([]RouteDiag, len(routes))

	for i, ri := range routes {
		handler := ""
		if len(ri.HandlerNames) > 0 {
			handler = ri.HandlerNames[len(ri.HandlerNames)-1] // 最後一個是實際 handler
		}

		_, hasSchema := schema.Global().Get(ri.Method, ri.Path)
		diags[i] = RouteDiag{
			Method:  ri.Method,
			Path:    ri.Path,
			Handler: handler,
			Schema:  hasSchema,
		}
	}

	return diags
}

// collectDatabase 收集資料庫資訊（安全遮蔽密碼）
func collectDatabase(cfg *config.Config, redact bool) *DatabaseDiag {
	diag := &DatabaseDiag{
		Driver:      cfg.Database.Driver,
		HasReplicas: len(cfg.Database.Replicas) > 0,
	}

	if redact {
		diag.Host = redactDSN(cfg.Database.DSN)
	} else {
		diag.Host = cfg.Database.DSN
	}

	return diag
}

// redactDSN 遮蔽 DSN 中的密碼
// "user:password@tcp(host:3306)/db" → "***@host:3306/db"
// "postgres://user:pass@host:5432/db" → "***@host:5432/db"
func redactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}

	// 嘗試 URL 格式 (postgres://user:pass@host/db)
	if u, err := url.Parse(dsn); err == nil && u.Host != "" {
		return "***@" + u.Host + u.Path
	}

	// 嘗試 MySQL 格式 (user:pass@tcp(host:port)/db)
	if atIdx := strings.Index(dsn, "@"); atIdx >= 0 {
		return "***" + dsn[atIdx:]
	}

	return "***"
}
