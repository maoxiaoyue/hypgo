// Package interceptor 提供 gRPC 中間件（對應 pkg/middleware）
package interceptor

import (
	"context"
	"runtime/debug"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Recovery 攔截 panic，回傳 Internal error 而非 crash
func Recovery(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				if log != nil {
					log.Error("gRPC panic recovered: %v\n%s", r, stack)
				}
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}
