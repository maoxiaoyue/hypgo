# Logger Package (`pkg/logger`)

`logger` 套件提供一套為 HypGo 量身打造、兼具輕量級與高效能的日誌記錄工具。它建立在 Go 標準庫 `log` 之上，並擴展了彩色輸出、多層級過濾和檔案自動輪轉（Log Rotation）等功能。

## 主要特色

- **多種日誌層級**: 支援 `DEBUG`, `INFO`, `NOTICE`, `WARN`, `ERROR`, `EMERGENCY` 等級，可輕鬆控制輸出顆粒度。
- **自動日誌輪轉**: 內建 `LogRotator`，可基於檔案大小或保存天數自動切分日誌檔案並清理舊日誌，避免磁碟爆滿。
- **高可讀的彩色終端輸出**: 若將日誌輸出到 `stdout`/`stderr`，便會自動啟用色彩標示級別及字距對齊，使開發體驗大幅提升。
- **支援上下文附帶鍵值 (Key-Value Logging)**: 提供簡便的寫法將參數變數印出，並會自動被格式化附加在日誌訊息後。
- **全局實例 (Singleton) 與靈活配置**: 透過 `InitLogger` 和 `GetLogger` 提供一組便於整併的全域日誌記錄器。

## 基礎使用

使用 `GetLogger()` 取得全局 `Logger`，即可在任何地方進行日誌記錄：

```go
package main

import (
    "github.com/maoxiaoyue/hypgo/pkg/logger"
)

func main() {
    log := logger.GetLogger()
    
    // 設定輸出層級 (DEBUG 以下層級皆會輸出)
    log.SetLevel(logger.DEBUG)

    // 基本日誌輸出
    log.Info("伺服器已啟動")
    
    // 輸出時夾帶變數
    log.Debug("接收到新的連線要求", "client_ip", "192.168.1.100", "port", 8080)
    
    // 發生錯誤
    log.Error("無法寫入資料庫: %v", err)
    
    // 發生致命錯誤，輸出後會直接 os.Exit(1)
    log.Fatal("系統核心元件遺失！")
}
```

## 日誌輪轉與檔案輸出

如果你想要將日誌儲存到檔案中，而不是終端機，可以設定輸出的路徑與過期機制：

```go
log := logger.GetLogger()

// 設定將日誌輸出到 logs/app.log 檔案中
log.SetFile("logs/app.log")

// 設定自動輪轉策略
log.SetRotator(&logger.LogRotator{
    filename:   "logs/app.log", // 與 SetFile 要輸出路徑相同
    maxSize:    10485760,        // 最大檔案大小（此為 10 MB）
    maxAge:     7,               // 保留幾天內的日誌
    maxBackups: 5,               // 最多保留幾份歷史日誌檔案
})
```
