package app

import (
    "context"
    "github.com/yourusername/hypgo/pkg/config"
    "github.com/yourusername/hypgo/pkg/database"
    "github.com/yourusername/hypgo/pkg/logger"
    "github.com/yourusername/hypgo/pkg/server"
)

type HypGo struct {
    Config   *config.Config
    Server   *server.Server
    DB       *database.DB
    Logger   *logger.Logger
    ctx      context.Context
    cancel   context.CancelFunc
}

func New(opts ...Option) (*HypGo, error) {
    options := &Options{
        ConfigPath: "config.yaml",
    }
    
    for _, opt := range opts {
        opt(options)
    }
    
    // 初始化配置
    cfg, err := config.Load(options.ConfigPath)
    if err != nil {
        return nil, err
    }
    
    // 初始化日誌
    log, err := logger.New(cfg.Logger)
    if err != nil {
        return nil, err
    }
    
    // 初始化資料庫
    db, err := database.New(cfg.Database)
    if err != nil {
        return nil, err
    }
    
    // 初始化伺服器
    srv, err := server.New(cfg.Server, log)
    if err != nil {
        return nil, err
    }
    
    ctx, cancel := context.WithCancel(context.Background())
    
    return &HypGo{
        Config: cfg,
        Server: srv,
        DB:     db,
        Logger: log,
        ctx:    ctx,
        cancel: cancel,
    }, nil
}

func (h *HypGo) Run() error {
    return h.Server.Start(h.ctx)
}

func (h *HypGo) Shutdown() error {
    h.cancel()
    return h.Server.Shutdown()
}
