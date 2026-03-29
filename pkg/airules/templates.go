package airules

import (
	"fmt"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/manifest"
)

// coreContent 生成所有工具共用的核心指令內容
func coreContent(m *manifest.Manifest) string {
	var sb strings.Builder

	sb.WriteString(`## Framework

- **HypGo**: Modern Go web framework (HTTP/1.1 + HTTP/2 + HTTP/3 QUIC)
- Go 1.24+, Bun ORM, Radix Tree router, WebSocket multi-protocol
- Module: github.com/maoxiaoyue/hypgo

## Project Structure

` + "```" + `
pkg/           Core packages (router, context, server, schema, errors, etc.)
cmd/hyp/       CLI tool
.hyp/          Auto-generated project context (manifest)
app/config/    Application configuration (config.yaml)
` + "```" + `

## Key Conventions

1. **Schema-first routes**: Always register routes with type metadata
   ` + "```go" + `
   r.Schema(schema.Route{
       Method: "POST", Path: "/api/users",
       Summary: "Create user",
       Input: CreateUserReq{}, Output: UserResp{},
   }).Handle(handler)
   ` + "```" + `

2. **Typed errors**: Use predefined error codes, not ad-hoc error responses
   ` + "```go" + `
   var ErrNotFound = errors.Define("E1001", 404, "Not found", "general")
   errors.AbortWithAppError(c, ErrNotFound.With("id", 42))
   ` + "```" + `

3. **Contract testing**: Validate handlers match their schema
   ` + "```go" + `
   contract.TestAll(t, router) // tests all schema-registered routes
   ` + "```" + `

## Build & Test

` + "```bash" + `
go build ./...
go test ./pkg/... -v
go vet ./pkg/...
` + "```" + `

## AI Collaboration — Read This First

- **Start here**: Read ` + "`.hyp/context.yaml`" + ` — it has all routes, types, and config in ~500 tokens
- **Before modifying shared packages**: Run ` + "`hyp impact <file>`" + ` to check affected dependents
- **After writing code**: Run ` + "`hyp chkcomment <file>`" + ` to ensure annotation completeness
- **Generate manifest**: ` + "`hyp context -o .hyp/manifest.yaml`" + `
- **Error codes**: Follow pattern E{category}{number} (e.g., E1001, E2001)
- **All schema routes have Input/Output types** — use them for code generation
`)

	// 動態注入路由資訊（如果 manifest 有資料）
	if m != nil && len(m.Routes) > 0 {
		sb.WriteString("\n## Current Routes\n\n")
		sb.WriteString("| Method | Path | Summary |\n")
		sb.WriteString("|--------|------|--------|\n")
		for _, r := range m.Routes {
			summary := r.Summary
			if summary == "" {
				summary = "-"
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", r.Method, r.Path, summary))
		}
	}

	return sb.String()
}

// generateAgentsMD 生成 AGENTS.md（Codex CLI, Cursor, Continue.dev, Aider, OpenHands）
func generateAgentsMD(m *manifest.Manifest) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	sb.WriteString(coreContent(m))
	return sb.String()
}

// generateGeminiMD 生成 GEMINI.md（Google Gemini CLI / AI Studio）
func generateGeminiMD(m *manifest.Manifest) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	sb.WriteString(coreContent(m))
	return sb.String()
}

// generateCopilotMD 生成 .github/copilot-instructions.md（GitHub Copilot）
func generateCopilotMD(m *manifest.Manifest) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	sb.WriteString(coreContent(m))
	return sb.String()
}

// generateCursorMDC 生成 .cursor/rules/hypgo.mdc（Cursor 新格式）
func generateCursorMDC(m *manifest.Manifest) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	// Cursor .mdc frontmatter
	sb.WriteString("---\n")
	sb.WriteString("description: HypGo framework conventions and AI collaboration rules\n")
	sb.WriteString("globs: \"**/*.go\"\n")
	sb.WriteString("alwaysApply: true\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	sb.WriteString(coreContent(m))
	return sb.String()
}

// generateWindsurfMD 生成 .windsurf/rules/hypgo.md（Windsurf）
// Windsurf 限制單檔 6,000 字元，因此使用精簡版
func generateWindsurfMD(m *manifest.Manifest) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")

	content := coreContent(m)
	// 如果超過 5,800 字元（留 200 給標記和標題），截斷路由表
	if len(content) > 5800 {
		// 找到 "## Current Routes" 並截斷
		if idx := strings.Index(content, "\n## Current Routes"); idx > 0 {
			content = content[:idx]
		}
	}
	sb.WriteString(content)
	return sb.String()
}
