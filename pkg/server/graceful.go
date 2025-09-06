package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ListenAndServeWithGracefulShutdown 啟動伺服器並支援優雅關閉
func (s *Server) ListenAndServeWithGracefulShutdown() error {
	// 應用預設中間件
	s.applyDefaultMiddlewares()

	// 啟動伺服器
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start()
	}()

	// 設置信號處理
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 等待關閉信號或錯誤
	select {
	case err := <-errChan:
		return err
	case <-quit:
		s.logger.Info("Received shutdown signal")

		// 優雅關閉
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Shutdown(ctx); err != nil {
			s.logger.Emergency("Server forced to shutdown: %v", err)
			return err
		}

		s.logger.Info("Server exited gracefully")
	}

	return nil
}
