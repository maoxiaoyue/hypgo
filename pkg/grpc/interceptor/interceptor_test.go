package interceptor

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockHandler 模擬正常 handler
func mockHandler(ctx context.Context, req interface{}) (interface{}, error) {
	return "ok", nil
}

// mockPanicHandler 模擬 panic 的 handler
func mockPanicHandler(ctx context.Context, req interface{}) (interface{}, error) {
	panic("test panic")
}

// mockErrorHandler 模擬回傳錯誤的 handler
func mockErrorHandler(ctx context.Context, req interface{}) (interface{}, error) {
	return nil, status.Error(codes.NotFound, "not found")
}

var testInfo = &grpc.UnaryServerInfo{
	FullMethod: "/test.Service/Method",
}

// --- Recovery ---

func TestRecoveryNormal(t *testing.T) {
	interceptor := Recovery(nil)
	resp, err := interceptor(context.Background(), nil, testInfo, mockHandler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != "ok" {
		t.Errorf("resp = %v, want ok", resp)
	}
}

func TestRecoveryPanic(t *testing.T) {
	interceptor := Recovery(nil)
	_, err := interceptor(context.Background(), nil, testInfo, mockPanicHandler)
	if err == nil {
		t.Fatal("expected error after panic")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("code = %v, want Internal", st.Code())
	}
}

// --- Logger ---

func TestLoggerNormal(t *testing.T) {
	interceptor := Logger(nil)
	resp, err := interceptor(context.Background(), nil, testInfo, mockHandler)
	if err != nil {
		t.Error(err)
	}
	if resp != "ok" {
		t.Error("wrong response")
	}
}

func TestLoggerError(t *testing.T) {
	interceptor := Logger(nil)
	_, err := interceptor(context.Background(), nil, testInfo, mockErrorHandler)
	if err == nil {
		t.Error("expected error")
	}
}

// --- Auth ---

func TestAuthSuccess(t *testing.T) {
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		if token == "valid-token" {
			return "user123", nil
		}
		return nil, errors.New("invalid")
	}

	interceptor := Auth(authFn)

	md := metadata.Pairs("authorization", "valid-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, nil, testInfo, func(ctx context.Context, req interface{}) (interface{}, error) {
		user, ok := UserFromContext(ctx)
		if !ok || user != "user123" {
			t.Error("user should be in context")
		}
		return "ok", nil
	})
	if err != nil {
		t.Error(err)
	}
	if resp != "ok" {
		t.Error("wrong response")
	}
}

func TestAuthMissingToken(t *testing.T) {
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		return nil, errors.New("invalid")
	}

	interceptor := Auth(authFn)
	_, err := interceptor(context.Background(), nil, testInfo, mockHandler)
	if err == nil {
		t.Fatal("expected error")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", st.Code())
	}
}

func TestAuthSkipMethod(t *testing.T) {
	authFn := func(ctx context.Context, token string) (interface{}, error) {
		return nil, errors.New("should not be called")
	}

	interceptor := Auth(authFn, "/test.Service/Method")
	resp, err := interceptor(context.Background(), nil, testInfo, mockHandler)
	if err != nil {
		t.Error(err)
	}
	if resp != "ok" {
		t.Error("skipped method should pass through")
	}
}

// --- RateLimit ---

func TestRateLimitAllow(t *testing.T) {
	interceptor := RateLimit(RateLimitConfig{
		MaxRequests: 5,
		Window:      time.Second,
	})

	for i := 0; i < 5; i++ {
		_, err := interceptor(context.Background(), nil, testInfo, mockHandler)
		if err != nil {
			t.Errorf("request %d should be allowed: %v", i, err)
		}
	}
}

func TestRateLimitExceeded(t *testing.T) {
	interceptor := RateLimit(RateLimitConfig{
		MaxRequests: 2,
		Window:      time.Second,
	})

	// 前 2 個通過
	interceptor(context.Background(), nil, testInfo, mockHandler)
	interceptor(context.Background(), nil, testInfo, mockHandler)

	// 第 3 個被拒
	_, err := interceptor(context.Background(), nil, testInfo, mockHandler)
	if err == nil {
		t.Fatal("should be rate limited")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("code = %v, want ResourceExhausted", st.Code())
	}
}

// --- UserFromContext ---

func TestUserFromContextEmpty(t *testing.T) {
	_, ok := UserFromContext(context.Background())
	if ok {
		t.Error("should return false for empty context")
	}
}
