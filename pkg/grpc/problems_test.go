package grpc

import (
	"context"
	"sync/atomic"
	"testing"

	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// DisableDefaultRecovery：使用者掛在最外層的 panic 處理器收得到 panic。
// 歷史 bug：預設的 Recovery 一律掛上且無法關閉，以 grpc.UnaryInterceptor 掛的 Sentry / 指標處理器
// 只看到一個 Internal 錯誤
func TestDisableDefaultRecovery(t *testing.T) {
	run := func(disable bool) (sawPanic bool, err error) {
		var saw atomic.Bool
		outer := func(ctx context.Context, req any, _ *gogrpc.UnaryServerInfo, h gogrpc.UnaryHandler) (resp any, err error) {
			defer func() {
				if r := recover(); r != nil {
					saw.Store(true)
					err = status.Error(codes.Aborted, "handled by the user's interceptor")
				}
			}()
			return h(ctx, req)
		}
		s := New(Config{Addr: "127.0.0.1:0", DisableDefaultRecovery: disable}, nil, gogrpc.UnaryInterceptor(outer))
		var impl any = struct{}{}
		s.GRPCServer().RegisterService(&testSvc, &impl)
		startTestServer(t, s)
		cc, cerr := gogrpc.NewClient(s.Addr(), gogrpc.WithTransportCredentials(insecure.NewCredentials()))
		if cerr != nil {
			t.Fatal(cerr)
		}
		defer func() { cc.Close(); s.Stop() }()
		err = cc.Invoke(context.Background(), "/test.Svc/Boom", &emptypb.Empty{}, &emptypb.Empty{})
		return saw.Load(), err
	}
	if saw, err := run(true); !saw || status.Code(err) != codes.Aborted {
		t.Errorf("with DisableDefaultRecovery: user interceptor saw the panic = %v, err = %v", saw, err)
	}
	// 預設：內建的 Recovery 先處理，回 Internal
	if saw, err := run(false); saw || status.Code(err) != codes.Internal {
		t.Errorf("default: user interceptor saw the panic = %v, err = %v", saw, err)
	}
}
