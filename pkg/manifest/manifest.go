// Package manifest 提供 Project Manifest 自動生成功能
// 掃描 HypGo 應用程式的路由、中間件、設定，產出機器可讀的 YAML/JSON 描述
// 讓 AI 能以最少 token 掌握專案全貌
package manifest

import (
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/schema"
)

// Manifest 描述整個 HypGo 應用程式的結構
type Manifest struct {
	Version     string          `json:"version" yaml:"version"`
	Framework   string          `json:"framework" yaml:"framework"`
	GeneratedAt time.Time       `json:"generated_at" yaml:"generated_at"`
	Server      ServerInfo      `json:"server" yaml:"server"`
	Routes      []RouteManifest  `json:"routes" yaml:"routes"`
	Models      []ModelManifest  `json:"models,omitempty" yaml:"models,omitempty"`
	Middleware  []string         `json:"middleware,omitempty" yaml:"middleware,omitempty"`
	Database    *DatabaseInfo    `json:"database,omitempty" yaml:"database,omitempty"`
}

// ServerInfo 描述伺服器配置
type ServerInfo struct {
	Addr     string `json:"addr" yaml:"addr"`
	Protocol string `json:"protocol" yaml:"protocol"`
	TLS      bool   `json:"tls" yaml:"tls"`
}

// RouteManifest 描述單一路由（含 schema metadata，支援多協議）
type RouteManifest struct {
	// 多協議支援
	Protocol     string         `json:"protocol,omitempty" yaml:"protocol,omitempty"`       // "rest", "grpc", "bot", "mcp", "websocket", "cli"
	Command      string         `json:"command,omitempty" yaml:"command,omitempty"`          // 非 REST 命令標識
	Platform     string         `json:"platform,omitempty" yaml:"platform,omitempty"`        // bot 專用平台

	// REST 欄位
	Method       string         `json:"method,omitempty" yaml:"method,omitempty"`
	Path         string         `json:"path,omitempty" yaml:"path,omitempty"`
	HandlerNames []string       `json:"handler_names,omitempty" yaml:"handler_names,omitempty"`

	// 共用 metadata
	Summary      string         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description  string         `json:"description,omitempty" yaml:"description,omitempty"`
	Tags         []string       `json:"tags,omitempty" yaml:"tags,omitempty"`
	InputType    string              `json:"input_type,omitempty" yaml:"input_type,omitempty"`
	OutputType   string              `json:"output_type,omitempty" yaml:"output_type,omitempty"`
	Responses    map[int]string      `json:"responses,omitempty" yaml:"responses,omitempty"`

	// 推斷/增強的欄位（IncludeFields 開啟時填入）
	InputFields  []schema.FieldInfo  `json:"input_fields,omitempty" yaml:"input_fields,omitempty"`
	OutputFields []schema.FieldInfo  `json:"output_fields,omitempty" yaml:"output_fields,omitempty"`
}

// DatabaseInfo 描述資料庫配置
type DatabaseInfo struct {
	Driver      string `json:"driver" yaml:"driver"`
	HasReplicas bool   `json:"has_replicas" yaml:"has_replicas"`
}

// ModelManifest 描述一個資料模型（對應資料庫資料表）
type ModelManifest struct {
	Name   string          `json:"name" yaml:"name"`
	Table  string          `json:"table" yaml:"table"`
	Fields []FieldManifest `json:"fields" yaml:"fields"`
}

// FieldManifest 描述一個資料欄位
type FieldManifest struct {
	Name          string `json:"name" yaml:"name"`
	GoType        string `json:"go_type" yaml:"go_type"`
	SQLType       string `json:"sql_type,omitempty" yaml:"sql_type,omitempty"`
	PrimaryKey    bool   `json:"pk,omitempty" yaml:"pk,omitempty"`
	AutoIncrement bool   `json:"auto_increment,omitempty" yaml:"auto_increment,omitempty"`
	NotNull       bool   `json:"not_null,omitempty" yaml:"not_null,omitempty"`
	Unique        bool   `json:"unique,omitempty" yaml:"unique,omitempty"`
	Default       string `json:"default,omitempty" yaml:"default,omitempty"`
}
