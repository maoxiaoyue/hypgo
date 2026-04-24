# HypGo 專案 — AI Agents 指示

> 標準：[agents.md](https://agents.md)
> 適用：Codex CLI、Cursor（新版）、Aider、其他支援 AGENTS.md 的工具

本專案的完整 AI 協作規範定義於：

**[`config/AI_CODING_RULES.md`](config/AI_CODING_RULES.md)**

在生成或修改任何 Go 程式碼前，請先讀取該檔案並嚴格遵守其中所有規則。

## 最關鍵 3 條（摘要）

1. **所有 func 必須帶 `@ai` 註解區塊**（exported 需含 purpose / input / output / sideeffect）
2. **日期格式強制 `YYYY-MM-DD`**，模型名稱用官方識別符（如 `gpt-5-codex`）
3. **Schema-first**：REST 用 `r.Schema(...)`、gRPC 用 `schema.RegisterGRPC(...)`，不直接寫 Route

完整規則、範例、禁止事項請參閱 `config/AI_CODING_RULES.md`。

## 建置與測試

```bash
go build ./...
go test ./pkg/...
go vet ./...
hyp chkcomment ./...
```
