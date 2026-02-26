# HypGo CLI (cmd/hyp) 說明文件

`hyp` 是針對 HypGo 框架所設計的命令列工具（CLI），以快速建立、管理及開發 HypGo 專案。

## 功能概覽

`hyp` 工具提供了多項子命令以協助專案開發：

- **專案初始化**：快速建立完整專案或 API 專案
- **開發工具**：支援熱重載（Hot reload）的開發伺服器
- **代碼生成**：快速生成 Controller、Model 或 Service 的模板代碼
- **管理與維運**：提供應用程式健康檢查、Docker 映像檔建立等功能
- **外掛系統**：列出與管理各種可用外掛（如 Elasticsearch, Kafka, Cassandra, RabbitMQ）

## 安裝與執行

通常可以透過 `go install` 安裝，或者直接在專案目錄下編譯並執行：

```bash
go build -o hyp main.go
./hyp --help
```

## 可用子命令

### 1. `new`
建立一個具備完整結構（包含 Controller、Model、Service 與預設歡迎 HTML 頁面）的全新 HypGo 專案。

**使用方法：**
```bash
hyp new [project-name]
```

### 2. `api`
建立一個以 API 為主的全新 HypGo 專案（不包含靜態檔案與樣板設定）。

**使用方法：**
```bash
hyp api [project-name]
```

### 3. `run`
以開發模式啟動目前的 HypGo 應用程式，並支援熱重載（Hot reload）功能。

**使用方法：**
```bash
hyp run
```

### 4. `list`
列出所有可以被安裝到 HypGo 專案中的可用外掛系統組件。

**使用方法：**
```bash
hyp list
```

### 5. `restart`
熱重啟（Hot restart）執行中的 HypGo 應用程式，實現零停機時間的無縫重啟。

**使用方法：**
```bash
hyp restart
```

### 6. `docker`
依據 `config.yaml` 內的設定，為當前 HypGo 專案構建 Docker 映像檔。

**使用方法：**
```bash
hyp docker
```

### 7. `generate`
為專案快速生成各種類型的樣板代碼檔案，例如 Controller、Model 或是 Service。

**使用方法：**
```bash
hyp generate [type] [name]
```

### 8. `health`
檢查當前執行中的 HypGo 應用程式健康狀態。

**使用方法：**
```bash
hyp health
```

## 外掛生態系統

`hyp` CLI 內建了 `PluginRegistry` 以管理各種第三方組件的支持：
- **搜索引擎**：如 `elasticsearch`
- **訊息佇列**：如 `kafka`、`rabbitmq`
- **NoSQL 資料庫**：如 `cassandra`

可使用 `hyp list` 命令來檢視這些可用的外掛清單與相關資訊。
