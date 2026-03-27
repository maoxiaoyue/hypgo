// Package manifest 提供 Project Manifest 自動生成功能
// 掃描 HypGo 應用程式的路由、中間件、設定，產出機器可讀的 YAML/JSON 描述
// 讓 AI 能以最少 token 掌握專案全貌
package manifest

import "time"

// Manifest 描述整個 HypGo 應用程式的結構
type Manifest struct {
	Version     string          `json:"version" yaml:"version"`
	Framework   string          `json:"framework" yaml:"framework"`
	GeneratedAt time.Time       `json:"generated_at" yaml:"generated_at"`
	Server      ServerInfo      `json:"server" yaml:"server"`
	Routes      []RouteManifest `json:"routes" yaml:"routes"`
	Middleware  []string        `json:"middleware,omitempty" yaml:"middleware,omitempty"`
	Database    *DatabaseInfo   `json:"database,omitempty" yaml:"database,omitempty"`
}

// ServerInfo 描述伺服器配置
type ServerInfo struct {
	Addr     string `json:"addr" yaml:"addr"`
	Protocol string `json:"protocol" yaml:"protocol"`
	TLS      bool   `json:"tls" yaml:"tls"`
}

// RouteManifest 描述單一路由（含 schema metadata）
type RouteManifest struct {
	Method       string         `json:"method" yaml:"method"`
	Path         string         `json:"path" yaml:"path"`
	HandlerNames []string       `json:"handler_names" yaml:"handler_names"`
	Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	Tags         []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
	InputType    string         `json:"input_type,omitempty" yaml:"input_type,omitempty"`
	OutputType   string         `json:"output_type,omitempty" yaml:"output_type,omitempty"`
	Responses    map[int]string `json:"responses,omitempty" yaml:"responses,omitempty"`
}

// DatabaseInfo 描述資料庫配置
type DatabaseInfo struct {
	Driver      string `json:"driver" yaml:"driver"`
	HasReplicas bool   `json:"has_replicas" yaml:"has_replicas"`
}
