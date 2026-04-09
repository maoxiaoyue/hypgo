package interceptor

import (
	"context"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Logger 記錄每個 gRPC 呼叫的方法、耗時、狀態碼
func Logger(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		st, _ := status.FromError(err)

		if log != nil {
			if err != nil {
				log.Warning("gRPC %s [%s] %v (%s)", info.FullMethod, st.Code(), err, duration)
			} else {
				log.Info("gRPC %s [%s] (%s)", info.FullMethod, st.Code(), duration)
			}
		}

		return resp, err
	}
}
