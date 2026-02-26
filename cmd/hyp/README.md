# HypGo CLI (`cmd/hyp`)

HypGo CLI (`hyp`) 是 HypGo 框架專用的命令列工具，可以用於快速建立、管理和運行你的 HypGo 應用程式。

## 安裝

你可以透過 `go install` 安裝 CLI 工具：

```bash
go install github.com/maoxiaoyue/hypgo/cmd/hyp@latest
```

確保你的 `$GOPATH/bin` 已加入到系統的 `PATH` 環境變數中。

## 可用指令

### `hyp new [project-name]`
建立一個包含完整資料夾結構（controllers, models, services 以及 welcome HTML 頁面）的全新 HypGo 專案。

- **範例**: `hyp new myapp`

### `hyp api [project-name]`
建立一個僅有 API 結構的專案，不包含處理靜態檔案與樣板的設定。

- **範例**: `hyp api myapi`

### `hyp run`
啟動 HypGo 應用程式。在開發模式下這可能會提供 Hot Reload 的功能（視設定與環境而定）。

- **範例**: `hyp run`

### `hyp restart`
執行應用程式的熱重啟（Hot Restart），以達成零停機部署。此指令在 Unix 系統上使用 `SIGUSR2` 訊號實作，目前 **不支援 Windows 系統**。

- **範例**: `hyp restart`

### `hyp docker`
根據 `config.yaml` 內的設定檔，為當前 HypGo 專案建立 Docker Image。

- **範例**: `hyp docker`

### `hyp generate [type] [name]`
自動生成 boilerplate 的模板程式碼。支援的 `[type]` 包含 controllers, models, 或是 services 等。

- **範例**:
  - `hyp generate controller user`
  - `hyp generate model order`

### `hyp health`
檢查當前運行中的 HypGo 應用程式之健康狀態。

- **範例**: `hyp health`

### `hyp list`
列出所有可用的 HypGo 擴充套件或插件（Plugins），例如 `elasticsearch`, `kafka`, `cassandra`, `rabbitmq`。

- **範例**: `hyp list`

## 確認安裝

輸入以下指令以確認您安裝的 CLI 版本：

```bash
hyp --version
```
> 輸出將顯示 `HypGo CLI [版本號]`
