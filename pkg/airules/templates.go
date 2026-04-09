package airules

import (
	"fmt"
	"strings"

	"github.com/maoxiaoyue/hypgo/pkg/manifest"
)

func coreContent(m *manifest.Manifest, opts Options) string {
	var sb strings.Builder

	sb.WriteString(`## Framework

- **HypGo**: Modern Go web framework (HTTP/1.1 + HTTP/2 + HTTP/3 QUIC)
- Go 1.24+, Bun ORM, Radix Tree router, WebSocket multi-protocol
- Module: github.com/maoxiaoyue/hypgo

## Project Structure

`)
	sb.WriteString("```\n")
	sb.WriteString("pkg/           Core packages (router, context, server, schema, errors, etc.)\n")
	sb.WriteString("cmd/hyp/       CLI tool\n")
	sb.WriteString(".hyp/          Auto-generated project context (manifest)\n")
	sb.WriteString("app/config/    Application configuration (config.yaml)\n")
	sb.WriteString("```\n")

	sb.WriteString(`
## Key Conventions

1. **Schema-first routes**: Always register routes with type metadata
`)
	sb.WriteString("   ```go\n")
	sb.WriteString("   r.Schema(schema.Route{\n")
	sb.WriteString("       Method: \"POST\", Path: \"/api/users\",\n")
	sb.WriteString("       Summary: \"Create user\",\n")
	sb.WriteString("       Input: CreateUserReq{}, Output: UserResp{},\n")
	sb.WriteString("   }).Handle(handler)\n")
	sb.WriteString("   ```\n")

	sb.WriteString(`
2. **Typed errors**: Use predefined error codes, not ad-hoc responses
`)
	sb.WriteString("   ```go\n")
	sb.WriteString("   var ErrNotFound = errors.Define(\"E1001\", 404, \"Not found\", \"general\")\n")
	sb.WriteString("   errors.AbortWithAppError(c, ErrNotFound.With(\"id\", 42))\n")
	sb.WriteString("   ```\n")

	sb.WriteString(`
3. **Contract testing**: Validate handlers match their schema
`)
	sb.WriteString("   ```go\n")
	sb.WriteString("   contract.TestAll(t, router) // tests all schema-registered routes\n")
	sb.WriteString("   ```\n")

	sb.WriteString(`
## Build & Test

`)
	sb.WriteString("```bash\n")
	sb.WriteString("go build ./...\ngo test ./pkg/... -v\ngo vet ./pkg/...\n")
	sb.WriteString("```\n")

	sb.WriteString(`
## AI Collaboration

- **Start here**: Read ` + "`.hyp/context.yaml`" + ` — all routes, types, config in ~500 tokens
- **Before modifying shared packages**: Run ` + "`hyp impact <file>`" + `
- **After writing code**: Run ` + "`hyp chkcomment <file>`" + `
- **Generate manifest**: ` + "`hyp context -o .hyp/manifest.yaml`" + `
- **Error codes**: Pattern E{category}{number} (e.g., E1001, E2001)
- **All schema routes have Input/Output types** — use them for code generation
`)

	// 條件性加入 diff-log 指令（開啟時才寫入，關閉時省 token）
	if opts.DiffLogEnabled {
		sb.WriteString("- **After making changes**: Run " + "`hyp diff-log`" + " to log your changes to logs/ai.diff_YYYYMMDD.log\n")
	}

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

func generateAgentsMD(m *manifest.Manifest, opts Options) string {
	return autoGenMarker + "\n# HypGo Framework Instructions\n\n" + coreContent(m, opts)
}

func generateGeminiMD(m *manifest.Manifest, opts Options) string {
	return autoGenMarker + "\n# HypGo Framework Instructions\n\n" + coreContent(m, opts)
}

func generateCopilotMD(m *manifest.Manifest, opts Options) string {
	return autoGenMarker + "\n# HypGo Framework Instructions\n\n" + coreContent(m, opts)
}

func generateCursorMDC(m *manifest.Manifest, opts Options) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("---\n")
	sb.WriteString("description: HypGo framework conventions and AI collaboration rules\n")
	sb.WriteString("globs: \"**/*.go\"\n")
	sb.WriteString("alwaysApply: true\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	sb.WriteString(coreContent(m, opts))
	return sb.String()
}

func generateWindsurfMD(m *manifest.Manifest, opts Options) string {
	var sb strings.Builder
	sb.WriteString(autoGenMarker + "\n")
	sb.WriteString("# HypGo Framework Instructions\n\n")
	content := coreContent(m, opts)
	if len(content) > 5800 {
		if idx := strings.Index(content, "\n## Current Routes"); idx > 0 {
			content = content[:idx]
		}
	}
	sb.WriteString(content)
	return sb.String()
}
