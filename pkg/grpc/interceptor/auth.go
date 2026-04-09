package interceptor

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthFunc 驗證函式型別
// 接收 token 字串，回傳使用者資訊（存入 context）和錯誤
type AuthFunc func(ctx context.Context, token string) (interface{}, error)

// contextKey 避免 key 衝突
type contextKey string

const userContextKey contextKey = "grpc_user"

// Auth 驗證 interceptor
// 從 metadata 中提取 "authorization" token，呼叫 authFn 驗證
// 驗證成功時將使用者資訊存入 context
//
// 可跳過不需驗證的方法（如 Health check）：
//
//	Auth(authFn, "/grpc.health.v1.Health/Check")
func Auth(authFn AuthFunc, skipMethods ...string) grpc.UnaryServerInterceptor {
	skip := make(map[string]bool, len(skipMethods))
	for _, m := range skipMethods {
		skip[m] = true
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 跳過不需驗證的方法
		if skip[info.FullMethod] {
			return handler(ctx, req)
		}

		// 從 metadata 取得 token
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		tokens := md.Get("authorization")
		if len(tokens) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization token")
		}

		// 驗證
		user, err := authFn(ctx, tokens[0])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}

		// 將使用者資訊存入 context
		ctx = context.WithValue(ctx, userContextKey, user)
		return handler(ctx, req)
	}
}

// UserFromContext 從 context 取出使用者資訊（Auth interceptor 存入的）
func UserFromContext(ctx context.Context) (interface{}, bool) {
	user := ctx.Value(userContextKey)
	return user, user != nil
}
