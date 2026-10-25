package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/migrate"
	"github.com/quic-go/quic-go/http3"
)

func regressCfg(addr string) *config.Config {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = addr
	cfg.Server.DisableAutoSync = true
	return &cfg
}

// selfSignedCert 在 dir 寫入 localhost 的自簽憑證，回傳 cert / key 路徑
func selfSignedCert(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return certFile, keyFile
}

// freeUDPPort 回傳目前空閒的 port（TCP 與 UDP 皆可用的機率高）
func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

// startServer 在背景 Start，等到 TCP 可連線；回傳 Start 的結果 channel
func startServer(t *testing.T, s *Server, port int) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			c.Close()
			return done
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestShutdownTimeoutForcesClose 迴歸測試（A1）：Shutdown 逾時後強制關閉進行中的連線，且無 data race。
// 歷史 bug：逾時只記一行 "forcing close"，連線照樣跑完；讀取錯誤變數與背景寫入競爭
func TestShutdownTimeoutForcesClose(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeUDPPort(t)
	s := New(regressCfg(fmt.Sprintf("127.0.0.1:%d", port)), logger.NewLogger())
	s.router.GET("/slow", func(c *hypcontext.Context) {
		time.Sleep(1500 * time.Millisecond)
		c.String(200, "done")
	})
	startServer(t, s, port)

	result := make(chan error, 1)
	go func() {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/slow", port))
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		result <- err
	}()
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); err == nil {
		t.Error("Shutdown past its deadline should report the timeout")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Error("in-flight request completed after the shutdown deadline; want the connection force-closed")
		}
	case <-time.After(time.Second):
		t.Error("in-flight request still running 1s after the shutdown deadline")
	}
}

// TestStartFailureCleansUp 迴歸測試（A5）：Start 失敗時移除 PID 檔並停止訊號監聽。
// 歷史 bug：PID 檔留下，signal.Notify 持續攔截 Ctrl+C
func TestStartFailureCleansUp(t *testing.T) {
	t.Chdir(t.TempDir())
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	s := New(regressCfg(busy.Addr().String()), logger.NewLogger())
	if err := s.Start(); err == nil {
		t.Fatal("Start on a busy port should fail")
	}
	if _, err := os.Stat(pidFilePath); err == nil {
		t.Error("PID file left behind after a failed Start")
	}
	select {
	case <-s.shutdownChan:
	case <-time.After(time.Second):
		t.Error("signal handling still active after a failed Start")
	}
}

// TestHTTP1ModeDoesNotNegotiateH2 迴歸測試（A6）：protocol: http1 加上 TLS 只提供 HTTP/1.1。
// 歷史 bug：ServeTLS 在 TLSNextProto 為 nil 時自動啟用 h2
func TestHTTP1ModeDoesNotNegotiateH2(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	port := freeUDPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
	s := New(cfg, logger.NewLogger())
	s.router.GET("/", func(c *hypcontext.Context) { c.String(200, "ok") })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
		ForceAttemptHTTP2: true,
	}}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ProtoMajor != 1 {
		t.Errorf("negotiated %s, want HTTP/1.1", resp.Proto)
	}
}

// TestAltSvcOnlyAfterH3Binds 迴歸測試（A4）：HTTP/3 綁定失敗時不廣告 Alt-Svc；綁定成功後只帶埠號。
// 歷史 bug：HTTP/3 失敗仍廣告，主機直接取 Addr（綁 0.0.0.0 時叫客戶端連往 0.0.0.0）
func TestAltSvcOnlyAfterH3Binds(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	altSvc := func(udpBusy bool) (string, int) {
		port := freeUDPPort(t)
		if udpBusy {
			pc, err := net.ListenPacket("udp", fmt.Sprintf("0.0.0.0:%d", port))
			if err != nil {
				t.Fatal(err)
			}
			defer pc.Close()
		}
		cfg := regressCfg(fmt.Sprintf("0.0.0.0:%d", port))
		cfg.Server.Protocol = "auto"
		cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
		s := New(cfg, logger.NewLogger())
		s.router.GET("/", func(c *hypcontext.Context) { c.String(200, "ok") })
		startServer(t, s, port)
		defer s.Shutdown(context.Background())
		if !udpBusy {
			deadline := time.Now().Add(2 * time.Second)
			for s.altSvc.Load() == nil && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		} else {
			time.Sleep(200 * time.Millisecond)
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Alt-Svc"), port
	}

	if got, _ := altSvc(true); got != "" {
		t.Errorf("HTTP/3 failed to bind but Alt-Svc = %q", got)
	}
	got, port := altSvc(false)
	if want := fmt.Sprintf(`h3=":%d"; ma=86400`, port); got != want {
		t.Errorf("Alt-Svc = %q, want %q", got, want)
	}
}

// TestMaxHandlersLimitsConcurrency 迴歸測試（A8）：max_handlers 限制同時執行的 handler 數。
// 歷史 bug：設定只傳給未實作的 http2.Server.MaxHandlers，沒有任何效果
func TestMaxHandlersLimitsConcurrency(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeUDPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.MaxHandlers = 2
	s := New(cfg, logger.NewLogger())
	var running, peak atomic.Int32
	release := make(chan struct{})
	s.router.GET("/work", func(c *hypcontext.Context) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		c.String(200, "ok")
	})
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/work", port)); err == nil {
				resp.Body.Close()
			}
		}()
	}
	time.Sleep(300 * time.Millisecond)
	if got := running.Load(); got != 2 {
		t.Errorf("%d handlers running with max_handlers=2", got)
	}
	close(release)
	wg.Wait()
	if got := peak.Load(); got != 2 {
		t.Errorf("peak concurrency = %d, want 2", got)
	}
}

// TestRestartedChildWaitsForUDPPort 迴歸測試（A3）：重啟產生的子行程在父行程釋放 UDP 埠後完成綁定。
// 歷史 bug：子行程只試一次，父行程仍占著埠 → 重啟後 HTTP/3 永久消失
func TestRestartedChildWaitsForUDPPort(t *testing.T) {
	parent, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := New(regressCfg(parent.LocalAddr().String()), logger.NewLogger())
	s.restarted = true
	got := make(chan error, 1)
	go func() {
		pc, err := s.listenUDP()
		if pc != nil {
			pc.Close()
		}
		got <- err
	}()
	time.Sleep(300 * time.Millisecond)
	parent.Close() // 父行程結束、釋放埠
	select {
	case err := <-got:
		if err != nil {
			t.Errorf("restarted child failed to bind after the port was released: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("restarted child did not bind after the port was released")
	}

	s2 := New(regressCfg("127.0.0.1:1"), logger.NewLogger()) // 非重啟：失敗立即回報
	busy, _ := net.ListenPacket("udp", "127.0.0.1:0")
	defer busy.Close()
	s2.config.Server.Addr = busy.LocalAddr().String()
	if _, err := s2.listenUDP(); err == nil {
		t.Error("a non-restarted process should fail immediately on a busy port")
	}
}

// TestHTTP3ShutdownIsGraceful 迴歸測試（A1）：HTTP/3 優雅關閉時進行中的請求可以完成。
// 歷史 bug：以 h3.Close() 直接中斷
func TestHTTP3ShutdownIsGraceful(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	port := freeUDPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http3"
	cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
	s := New(cfg, logger.NewLogger())
	s.router.GET("/slow", func(c *hypcontext.Context) {
		time.Sleep(500 * time.Millisecond)
		c.String(200, "done")
	})
	go s.Start()
	deadline := time.Now().Add(3 * time.Second)
	for s.h3Server.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	tr := &http3.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	defer tr.Close()
	result := make(chan string, 1)
	go func() {
		resp, err := (&http.Client{Transport: tr}).Get(fmt.Sprintf("https://127.0.0.1:%d/slow", port))
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			result <- "error: " + err.Error()
			return
		}
		result <- string(b)
	}()
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s.Shutdown(ctx)
	if got := <-result; got != "done" {
		t.Errorf("in-flight HTTP/3 request during graceful shutdown: %s", got)
	}
}

// TestHTTP3ModeAlsoServesTCP 迴歸測試（C）：protocol: http3 同時監聽 TCP 並廣告 Alt-Svc。
// 歷史 bug：只監聽 UDP，瀏覽器沒有 TCP 回應可以讀到 Alt-Svc，無從發現 HTTP/3
func TestHTTP3ModeAlsoServesTCP(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	port := freeUDPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http3"
	cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
	s := New(cfg, logger.NewLogger())
	s.router.GET("/", func(c *hypcontext.Context) { c.String(200, "ok") })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())
	deadline := time.Now().Add(2 * time.Second)
	for s.altSvc.Load() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	tlsConf := &tls.Config{InsecureSkipVerify: true}
	url := fmt.Sprintf("https://127.0.0.1:%d/", port)
	resp, err := (&http.Client{Transport: &http.Transport{TLSClientConfig: tlsConf}}).Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if want := fmt.Sprintf(`h3=":%d"; ma=86400`, port); resp.Header.Get("Alt-Svc") != want {
		t.Errorf("TCP Alt-Svc = %q, want %q", resp.Header.Get("Alt-Svc"), want)
	}

	tr := &http3.Transport{TLSClientConfig: tlsConf}
	defer tr.Close()
	resp, err = (&http.Client{Transport: tr}).Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Proto != "HTTP/3.0" {
		t.Errorf("Proto = %s, want HTTP/3.0", resp.Proto)
	}
}

// TestHTTP3ModeFailsWithoutUDP protocol: http3 時 HTTP/3 起不來就整個 Start 失敗並停止 TCP，
// 不像 auto 默默退回只有 TCP
func TestHTTP3ModeFailsWithoutUDP(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	port := freeUDPPort(t)
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http3"
	cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
	s := New(cfg, logger.NewLogger())

	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start() = nil, want the UDP bind error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start() kept running although HTTP/3 could not bind")
	}
	if c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		c.Close()
		t.Error("TCP still accepting after Start() failed")
	}
}

// regressWidget 供 TestStartMigrateOutWritesSchema 註冊到 migrate.GlobalRegistry
type regressWidget struct {
	ID   int64  `bun:"id,pk,autoincrement"`
	Name string `bun:"name,notnull"`
}

// TestStartMigrateOutWritesSchema 迴歸測試（CLI A5）：設定 HYPGO_MIGRATE_OUT 時 Start 寫出已註冊 model 的 schema 後立即返回、不監聽。
// 歷史 bug：hyp migrate 讀 hyp 自己行程的 GlobalRegistry（永遠是空的），使用者專案沒有任何方式把 model 交給它
func TestStartMigrateOutWritesSchema(t *testing.T) {
	t.Chdir(t.TempDir())
	migrate.GlobalRegistry().Register((*regressWidget)(nil))
	out := filepath.Join(t.TempDir(), "schema.json")
	t.Setenv("HYPGO_MIGRATE_OUT", out)

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := busy.Addr().String()
	busy.Close()
	s := New(regressCfg(addr), logger.NewLogger())

	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		s.Shutdown(context.Background())
		t.Fatal("Start did not return: it started serving instead of writing the model schema")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"name":"regress_widget"`) {
		t.Errorf("schema output missing registered model: %s", data)
	}
	if _, err := os.Stat(pidFilePath); err == nil {
		t.Error("PID file written in migrate-out mode")
	}
}
