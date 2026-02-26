# JSON Package (`pkg/json`)

`json` 套件為 HypGo 提供進階 JSON 處理能力，基於 `encoding/json` 但是整合了 `go-playground/validator/v10`，在序列化或是反序列化的過程中進行資料檢查或是驗證。

## 主要特色

- **強大的 Struct 驗證**: 將 `validator` 內建於 `ValidatedUnmarshal` 中，解析即完成校驗。
- **支援 JSON Schema**: 提供像 JSON Schema 一樣的屬性驗證功能 (`ValidateWithSchema`)，包括必填欄位、長度、正則表達式或 Enum 限定。
- **類型自動轉型 (Typed Unmarshal)**: 提供 `TypedUnmarshal` 自動適配目標結構體支援的類型轉換操作。
- **統一錯誤格式**: 將 `validator.ValidationErrors` 自動整理為結構化的 `ValidationError`，方便 API 將明確的錯誤訊息轉換為 Response 拋回給前端。
- **自定義驗證**: 提供 `RegisterValidation` 自訂義標籤讓開發者可以輕鬆擴充所需的驗證邏輯。

## 基礎使用

### 結構體驗證解析

透過建立一個 `Validator`，我們可以在 `json.Unmarshal` 執行的當下利用 Struct Tag (`validate:"..."`) 驗證必填項、甚至 Email 格式等設定：

```go
package main

import (
	"fmt"
	"log"

	hypjson "github.com/maoxiaoyue/hypgo/pkg/json"
)

type UserRequest struct {
	Name  string `json:"name" validate:"required"`
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"min=18"`
}

func main() {
	v := hypjson.NewValidator()

	data := []byte(`{"name": "Alice", "email": "alice@example", "age": 16}`)

	var req UserRequest
	err := v.ValidatedUnmarshal(data, &req)
	
	if err != nil {
		// Output validation errors securely
		errs := v.FormatErrors(err)
		for _, e := range errs {
			fmt.Printf("欄位 %s 發生錯誤: %s\n", e.Field, e.Message)
		}
	}
}
```

### JSON Schema 驗證

如果你想檢查動態或未定義成 Go Struct 的 JSON 資料，可以直接套用 Schema 定義：

```go
func main() {
	schema := hypjson.Schema{
		Type: "object",
		Required: []string{"username"},
		Properties: map[string]hypjson.Property{
			"username": {
				Type: "string",
				MinLength: hypjson.IntPtr(3),
			},
			"status": {
				Type: "string",
				Enum: []string{"active", "inactive"},
			},
		},
	}

	data := []byte(`{"username": "Al", "status": "unknown"}`)
	err := hypjson.ValidateWithSchema(data, schema)
	if err != nil {
		log.Println("Schema validation failed:", err)
	}
}
```

### 輔助方法

- `Marshal(v interface{})`: 快速輸出帶有排版縮排 (`Indent`) 的 JSON。
- `MarshalCompact(v interface{})`: 快速輸出無空格與換行的緊湊 JSON 格式。
