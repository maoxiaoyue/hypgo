package grpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// startTestServer 在背景 Start，等到 listener 就緒
func startTestServer(t *testing.T, s *Server) {
	t.Helper()
	go s.Start()
	deadline := time.Now().Add(2 * time.Second)
	for s.getListener() == nil {
		if time.Now().After(deadline) {
			t.Fatal("listener not ready within 2s")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestStartFailsOnBadTLSCert 迴歸測試（A1）：設定了 TLS 但憑證載入失敗時 Start 回傳錯誤。
// 歷史 bug：只記一行 log 照常啟動，以明文服務
func TestStartFailsOnBadTLSCert(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pem")
	os.WriteFile(bad, []byte("not a certificate"), 0o600)
	for name, tlsCfg := range map[string]*TLSConfig{
		"bad cert":   {CertFile: bad, KeyFile: bad},
		"empty path": {},
	} {
		s := New(Config{Addr: "127.0.0.1:0", TLS: tlsCfg}, nil)
		done := make(chan error, 1)
		go func() { done <- s.Start() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: Start() = nil, want TLS error", name)
			}
		case <-time.After(2 * time.Second):
			s.Stop()
			t.Errorf("%s: Start() kept serving without TLS", name)
		}
	}
}

// TestShutdownForcesStopWithOpenStream 迴歸測試（A2）：開著的 stream 不結束時，Shutdown 在 ctx 到期後強制停止。
// 歷史 bug：只有 GracefulStop，客戶端開著 health Watch 就永遠不返回
func TestShutdownForcesStopWithOpenStream(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0", EnableHealthCheck: true}, nil)
	startTestServer(t, s)
	cc, err := gogrpc.NewClient(s.Addr(), gogrpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	w, err := healthpb.NewHealthClient(cc).Watch(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Recv(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Shutdown(ctx) }()
	select {
	case err := <-done:
		if err != context.DeadlineExceeded {
			t.Errorf("Shutdown() = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(3 * time.Second):
		s.Stop()
		t.Fatal("Shutdown did not return after its context expired")
	}
}

// TestGracefulStopMarksAllServicesNotServing 迴歸測試（A3）：關閉時個別服務的健康狀態也變成 NOT_SERVING。
// 歷史 bug：只改整體狀態 ""，以 SetServingStatus 設定的個別服務仍回報 SERVING
func TestGracefulStopMarksAllServicesNotServing(t *testing.T) {
	s := New(Config{Addr: "127.0.0.1:0", EnableHealthCheck: true}, nil)
	s.SetServingStatus("svc", healthpb.HealthCheckResponse_SERVING)
	startTestServer(t, s)
	s.GracefulStop()
	for _, svc := range []string{"", "svc"} {
		resp, err := s.healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: svc})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Status != healthpb.HealthCheckResponse_NOT_SERVING {
			t.Errorf("service %q status after GracefulStop = %v, want NOT_SERVING", svc, resp.Status)
		}
	}
}
