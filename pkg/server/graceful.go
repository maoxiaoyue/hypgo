package server

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func (s *Server) ListenAndServeWithGracefulShutdown() error {
	// 啟動服務器
	errChan := make(chan error, 1)
	go func() {
		errChan <- s.Start()
	}()

	// 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errChan:
		return err
	case <-quit:
		s.logger.Info("Shutting down server...")

		// 設置關閉超時
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 關閉服務器
		if err := s.Shutdown(ctx); err != nil {
			s.logger.Emergency("Server forced to shutdown: %v", err)
			return err
		}
		s.logger.Info("Server exited gracefully")
	}

	return nil
}
