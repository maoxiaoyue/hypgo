package interceptor

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeStream 只提供 Context 的 ServerStream
type fakeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeStream) Context() context.Context { return f.ctx }

var testStreamInfo = &grpc.StreamServerInfo{FullMethod: "/test.Service/Stream"}

func withToken(token string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", token))
}

// TestStreamRecovery 迴歸測試（A4）：stream handler 的 panic 轉成 Internal error。
// 歷史 bug：只有 unary 版本，streaming RPC panic 讓整個行程崩潰
func TestStreamRecovery(t *testing.T) {
	err := StreamRecovery(nil)(nil, &fakeStream{ctx: context.Background()}, testStreamInfo,
		func(srv interface{}, ss grpc.ServerStream) error { panic("boom") })
	if status.Code(err) != codes.Internal {
		t.Errorf("err = %v, want Internal", err)
	}
}

// TestStreamAuth 迴歸測試（A4）：streaming RPC 也需驗證，handler 可由 ss.Context() 取得使用者。
// 歷史 bug：只有 unary 版本，掛了 Auth 的服務其 streaming RPC 完全不經驗證
func TestStreamAuth(t *testing.T) {
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		if token == "good" {
			return "alice", nil
		}
		return nil, errors.New("invalid token")
	}
	icpt := StreamAuth(authFn)

	called := false
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		called = true
		if user, ok := UserFromContext(ss.Context()); !ok || user != "alice" {
			t.Errorf("UserFromContext = %v, %v; want alice", user, ok)
		}
		return nil
	}
	if err := icpt(nil, &fakeStream{ctx: context.Background()}, testStreamInfo, handler); status.Code(err) != codes.Unauthenticated {
		t.Errorf("missing token: err = %v, want Unauthenticated", err)
	}
	if called {
		t.Error("handler ran without a token")
	}
	if err := icpt(nil, &fakeStream{ctx: withToken("good")}, testStreamInfo, handler); err != nil || !called {
		t.Errorf("valid token: err = %v, handler called = %v", err, called)
	}
}

// TestAuthPassesStatusErrors 迴歸測試（B1）：authFn 回傳的 status error 原樣傳給客戶端。
// 歷史 bug：一律包成 Unauthenticated，訊息帶著 "rpc error: code = ..." 前綴
func TestAuthPassesStatusErrors(t *testing.T) {
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		return nil, status.Error(codes.PermissionDenied, "admins only")
	}
	_, err := Auth(authFn)(withToken("x"), nil, testInfo, mockHandler)
	if st, _ := status.FromError(err); st.Code() != codes.PermissionDenied || st.Message() != "admins only" {
		t.Errorf("err = %v, want PermissionDenied \"admins only\"", err)
	}
}

// TestAuthStripsBearer 迴歸測試（B2）：authFn 收到的 token 不含 "Bearer " 前綴（不分大小寫）。
// 歷史 bug：原樣傳入 authorization 的值，與 HTTP 的 JWT 中間件不一致
func TestAuthStripsBearer(t *testing.T) {
	var got string
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		got = token
		return "u", nil
	}
	for _, header := range []string{"Bearer abc", "bearer abc", "abc"} {
		if _, err := Auth(authFn)(withToken(header), nil, testInfo, mockHandler); err != nil {
			t.Fatal(err)
		}
		if got != "abc" {
			t.Errorf("authorization %q: authFn got %q, want abc", header, got)
		}
	}
}

// TestRateLimitNoGoroutineLeak 迴歸測試（A5）：建立限流器不留下背景 goroutine。
// 歷史 bug：每次呼叫 RateLimit 都起一個永不結束的清理 goroutine
func TestRateLimitNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 50; i++ {
		RateLimit(RateLimitConfig{})
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+5 {
		t.Errorf("goroutines: %d before, %d after creating 50 rate limiters", before, after)
	}
}

// TestRateLimitSweepsExpiredKeys 過期的 key 在之後的請求觸發清理時被移除
func TestRateLimitSweepsExpiredKeys(t *testing.T) {
	key := "a"
	l := NewRateLimiter(RateLimitConfig{Window: time.Millisecond, CleanupInterval: 10 * time.Millisecond,
		KeyFunc: func(context.Context) string { return key }})
	l.allow(context.Background())
	time.Sleep(20 * time.Millisecond)
	key = "b"
	l.allow(context.Background()) // 觸發清理
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := l.clients.Load("a"); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expired key was not swept")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestRateLimiterKeyFuncAndSharedBudget KeyFunc 決定限流的 key（C1）；同一個 RateLimiter 的 Unary 與 Stream 共用額度
func TestRateLimiterKeyFuncAndSharedBudget(t *testing.T) {
	l := NewRateLimiter(RateLimitConfig{MaxRequests: 2, Window: time.Minute,
		KeyFunc: func(ctx context.Context) string {
			v := metadata.ValueFromIncomingContext(ctx, "authorization")
			return v[0]
		}})
	unary, stream := l.Unary(), l.Stream()
	streamHandler := func(srv interface{}, ss grpc.ServerStream) error { return nil }

	if _, err := unary(withToken("alice"), nil, testInfo, mockHandler); err != nil {
		t.Fatal(err)
	}
	if err := stream(nil, &fakeStream{ctx: withToken("alice")}, testStreamInfo, streamHandler); err != nil {
		t.Fatal(err)
	}
	if _, err := unary(withToken("alice"), nil, testInfo, mockHandler); status.Code(err) != codes.ResourceExhausted {
		t.Errorf("alice's 3rd call: err = %v, want ResourceExhausted", err)
	}
	if _, err := unary(withToken("bob"), nil, testInfo, mockHandler); err != nil {
		t.Errorf("bob has his own budget: err = %v", err)
	}
}
