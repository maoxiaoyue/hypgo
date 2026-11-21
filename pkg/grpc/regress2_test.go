package grpc

import (
	"context"
	"testing"
	"time"

	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// testSvc 手寫的 service：Boom 會 panic，Slow 等 slowFor 後回應，Stream 會 panic
const slowFor = 600 * time.Millisecond

func unary(name string, h func() (any, error)) gogrpc.MethodDesc {
	return gogrpc.MethodDesc{MethodName: name, Handler: func(_ any, ctx context.Context, dec func(any) error, ic gogrpc.UnaryServerInterceptor) (any, error) {
		in := new(emptypb.Empty)
		if err := dec(in); err != nil {
			return nil, err
		}
		handler := func(context.Context, any) (any, error) { return h() }
		if ic == nil {
			return handler(ctx, in)
		}
		return ic(ctx, in, &gogrpc.UnaryServerInfo{FullMethod: "/test.Svc/" + name}, handler)
	}}
}

var testSvc = gogrpc.ServiceDesc{
	ServiceName: "test.Svc",
	HandlerType: (*any)(nil),
	Methods: []gogrpc.MethodDesc{
		unary("Boom", func() (any, error) { panic("boom") }),
		unary("Slow", func() (any, error) { time.Sleep(slowFor); return &emptypb.Empty{}, nil }),
	},
	Streams: []gogrpc.StreamDesc{{
		StreamName:    "Stream",
		ServerStreams: true,
		Handler:       func(any, gogrpc.ServerStream) error { panic("stream boom") },
	}},
}

func startSvc(t *testing.T, cfg Config) (*Server, *gogrpc.ClientConn) {
	t.Helper()
	cfg.Addr = "127.0.0.1:0"
	s := New(cfg, nil)
	var impl any = struct{}{}
	s.GRPCServer().RegisterService(&testSvc, &impl)
	startTestServer(t, s)
	cc, err := gogrpc.NewClient(s.Addr(), gogrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cc.Close(); s.Stop() })
	return s, cc
}

// TestRecoveryIsInstalledByDefault：沒有自己掛 interceptor 的服務，handler panic 回 Internal。
// 先前 New 不掛 Recovery，而 grpc-go 不會 recover：panic 讓整個行程結束（這個測試會直接崩潰）
func TestRecoveryIsInstalledByDefault(t *testing.T) {
	_, cc := startSvc(t, Config{})
	err := cc.Invoke(context.Background(), "/test.Svc/Boom", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Internal {
		t.Errorf("unary panic: err = %v, want Internal", err)
	}
	st, err := cc.NewStream(context.Background(), &testSvc.Streams[0], "/test.Svc/Stream")
	if err != nil {
		t.Fatal(err)
	}
	st.SendMsg(&emptypb.Empty{})
	st.CloseSend()
	if err := st.RecvMsg(&emptypb.Empty{}); status.Code(err) != codes.Internal {
		t.Errorf("stream panic: err = %v, want Internal", err)
	}
	// 伺服器仍在服務
	if err := cc.Invoke(context.Background(), "/test.Svc/Slow", &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Errorf("server unusable after a panic: %v", err)
	}
}

// TestShutdownWaitsWhenAlreadyStopping：關閉進行中時再呼叫 Shutdown / GracefulStop，等關完才返回。
// 先前立刻回 nil，當時還有 RPC 在執行
func TestShutdownWaitsWhenAlreadyStopping(t *testing.T) {
	for name, second := range map[string]func(*Server){
		"Shutdown":     func(s *Server) { s.Shutdown(context.Background()) },
		"GracefulStop": func(s *Server) { s.GracefulStop() },
	} {
		s, cc := startSvc(t, Config{})
		rpcDone := make(chan struct{})
		go func() {
			cc.Invoke(context.Background(), "/test.Svc/Slow", &emptypb.Empty{}, &emptypb.Empty{})
			close(rpcDone)
		}()
		time.Sleep(100 * time.Millisecond)
		go s.GracefulStop()
		time.Sleep(50 * time.Millisecond)
		start := time.Now()
		second(s)
		if d := time.Since(start); d < 200*time.Millisecond {
			t.Errorf("%s returned after %v while an RPC needed about %v more", name, d, slowFor-150*time.Millisecond)
		}
		<-rpcDone
	}
	// Stop 之後的 GracefulStop 不會卡住
	s, _ := startSvc(t, Config{})
	s.Stop()
	done := make(chan struct{})
	go func() { s.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("GracefulStop after Stop never returned")
	}
}
