# HypGo 專案 — Claude 指示

本專案的完整 AI 協作規範定義於：

**[`.hyp/AI_CODING_RULES.md`](.hyp/AI_CODING_RULES.md)**

在生成或修改任何 Go 程式碼前，請先讀取該檔案並嚴格遵守其中所有規則。

## 註釋開關（必讀）

註釋規範由 **`.hyp/comment.yaml`** 控制。每次 session 開始生成程式碼前**先讀此檔**，依 toggle 決定要寫哪些註釋。檔案不存在時退回預設。

| Toggle | 預設 | 控制 |
|--------|------|------|
| `normal_comment` | **true** | 普通 Go doc 註釋 |
| `ai_comment` | **false** | `@ai:generated / @ai:purpose / @ai:input / @ai:output / @ai:sideeffect` |
| `think_comment` | **false** | func body 內 `//@ai:think intent= / special= / model=` |

Toggle 為 `false` 時**不寫**該類型註釋，即使下方規則描述為 MANDATORY。

## 最關鍵 3 條（摘要）

1. **註釋由 `.hyp/comment.yaml` 控制**（見上）；無檔則只寫普通註釋
2. **日期格式強制 `YYYY-MM-DD`**，模型名稱用官方識別符（如 `claude-opus-4-8`）
3. **Schema-first**：REST 用 `r.Schema(...)`、gRPC 用 `schema.RegisterGRPC(...)`，不直接寫 Route

完整規則、範例、禁止事項請參閱 `.hyp/AI_CODING_RULES.md`。

## 驗證

```bash
hyp chkcomment ./...                                # 檢查 @ai 標註
hyp chkcomment --fix ./...                          # 補骨架
hyp chkcomment --unintent <file>                    # 檢查 @ai:think 缺漏
hyp chkcomment --fixintent --llm .hyp/llm.yaml <f>  # LLM 自動補 @ai:think
```
