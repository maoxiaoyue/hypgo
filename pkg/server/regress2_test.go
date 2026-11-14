package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
)

// freeTCPPort 回傳目前空閒的 TCP port
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startExpectingError 執行 Start 並回傳它的錯誤；Start 沒有失敗（開始服務）時關閉 server 並讓測試失敗
func startExpectingError(t *testing.T, s *Server) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Start() }()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		s.Shutdown(context.Background())
		t.Fatal("Start kept serving instead of failing")
		return nil
	}
}

// h2cClient 回傳只講明文 HTTP/2（prior knowledge）的客戶端
func h2cClient() *http.Client {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	return &http.Client{Transport: &http.Transport{Protocols: p}}
}

// bufLogger 回傳輸出寫進緩衝區的 logger
func bufLogger() (*logger.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	log := logger.NewLogger()
	log.SetOutput(buf)
	return log, buf
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestH2CMaxHandlersCountsRequests 迴歸測試：明文 HTTP/2 的 max_handlers 以請求計數。
// 歷史 bug：semaphore 包在 h2c.NewHandler 外層，整條連線只算一個——連線上的請求不受限，
// 閒置的連線卻佔住名額，名額用完後其他請求永遠等不到
func TestH2CMaxHandlersCountsRequests(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http2"
	cfg.Server.MaxHandlers = 2
	s := New(cfg, logger.NewLogger())
	var running, peak atomic.Int32
	s.router.GET("/work", func(c *hypcontext.Context) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
		running.Add(-1)
		c.String(200, "ok")
	})
	s.router.GET("/fast", func(c *hypcontext.Context) { c.String(200, c.Request.Proto) })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	c1, c2 := h2cClient(), h2cClient()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := c1.Get(url + "/work"); err == nil {
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p != 2 {
		t.Errorf("peak concurrent handlers on one h2c connection = %d, want 2", p)
	}

	// 兩條閒置的 h2c 連線不佔名額
	for _, c := range []*http.Client{c1, c2} {
		r, err := c.Get(url + "/fast")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if string(b) != "HTTP/2.0" {
			t.Fatalf("h2c client was served %s", b)
		}
	}
	h1 := &http.Client{Timeout: 2 * time.Second}
	r, err := h1.Get(url + "/fast")
	if err != nil {
		t.Fatalf("HTTP/1.1 request blocked by idle h2c connections: %v", err)
	}
	r.Body.Close()
}

// TestH2CShutdownWaitsForRequests 迴歸測試：優雅關閉會等明文 HTTP/2 上進行中的請求。
// 歷史 bug：h2c 連線被 hijack，http.Server.Shutdown 不追蹤——Shutdown 立刻返回、
// Start 隨之返回，main 結束時請求直接遺失
func TestH2CShutdownWaitsForRequests(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http2"
	s := New(cfg, logger.NewLogger())
	s.router.GET("/slow", func(c *hypcontext.Context) {
		time.Sleep(700 * time.Millisecond)
		c.String(200, "done")
	})
	startServer(t, s, port)

	res := make(chan string, 1)
	go func() {
		r, err := h2cClient().Get(fmt.Sprintf("http://127.0.0.1:%d/slow", port))
		if err != nil {
			res <- "ERR " + err.Error()
			return
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		res <- string(b)
	}()
	time.Sleep(200 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case got := <-res:
		if got != "done" {
			t.Errorf("in-flight h2c request = %q, want done", got)
		}
	default:
		t.Error("Shutdown returned while the h2c request was still running")
	}
}

// TestH2CSettingsFromConfig 迴歸測試：max_concurrent_streams 套用到明文 HTTP/2
// （改用 net/http 內建實作後調校值仍要生效）
func TestH2CSettingsFromConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http2"
	cfg.Server.MaxConcurrentStreams = 37
	s := New(cfg, logger.NewLogger())
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	// 連線前言 + 空的 SETTINGS
	conn.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n\x00\x00\x00\x04\x00\x00\x00\x00\x00"))
	hdr := make([]byte, 9)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		t.Fatal(err)
	}
	if hdr[3] != 0x4 {
		t.Fatalf("first frame type = %d, want SETTINGS", hdr[3])
	}
	body := make([]byte, int(hdr[0])<<16|int(hdr[1])<<8|int(hdr[2]))
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatal(err)
	}
	got := uint32(0)
	for i := 0; i+6 <= len(body); i += 6 {
		if binary.BigEndian.Uint16(body[i:]) == 0x3 { // SETTINGS_MAX_CONCURRENT_STREAMS
			got = binary.BigEndian.Uint32(body[i+2:])
		}
	}
	if got != 37 {
		t.Errorf("SETTINGS_MAX_CONCURRENT_STREAMS = %d, want 37", got)
	}
}

// TestFailedStartKeepsOtherInstancePIDFile 迴歸測試：啟動失敗不動別的實例的 PID 檔。
// 歷史 bug：Start 一開始就把 PID 檔覆寫成自己的，綁埠失敗後再刪掉——
// 還在執行的第一個實例失去 hypgo.pid
func TestFailedStartKeepsOtherInstancePIDFile(t *testing.T) {
	t.Chdir(t.TempDir())
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	os.WriteFile(pidFilePath, []byte("999999"), 0o644)

	s := New(regressCfg(busy.Addr().String()), logger.NewLogger())
	if err := s.Start(); err == nil {
		t.Fatal("Start on a busy port should fail")
	}
	if data, _ := os.ReadFile(pidFilePath); string(data) != "999999" {
		t.Errorf("PID file = %q, want the other instance's 999999", data)
	}
}

// TestDefaultMiddlewaresWrapUserMiddleware 迴歸測試：預設的 Recovery / Logger / Security
// 包住使用者的全域中間件。歷史 bug：預設中間件附加在後面——被使用者中間件擋下的請求
// 不進存取日誌、沒有安全標頭，它 panic 時連線直接中斷
func TestDefaultMiddlewaresWrapUserMiddleware(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	log, buf := bufLogger()
	s := New(cfg, log)
	s.Use(func(c *hypcontext.Context) {
		switch c.Request.URL.Path {
		case "/deny":
			c.AbortWithStatus(401)
			return
		case "/boom":
			panic("middleware panic")
		}
		c.Next()
	})
	s.router.GET("/ok", func(c *hypcontext.Context) { c.String(200, "ok") })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	for path, want := range map[string]int{"/ok": 200, "/deny": 401, "/boom": 500} {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", path, resp.StatusCode, want)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: security headers missing", path)
		}
	}
	time.Sleep(50 * time.Millisecond)
	for _, path := range []string{"/ok", "/deny", "/boom"} {
		if !strings.Contains(buf.String(), path) {
			t.Errorf("%s is missing from the access log", path)
		}
	}
}

// TestHTTP3ModeUsesSameDefaultMiddlewares 迴歸測試：protocol: http3 與其他模式用同一組預設中間件。
// 歷史 bug：http3 換成另一組（沒有安全標頭與 CORS，多了每 IP 100 req/s 的限流），
// 而該模式同樣服務 TCP
func TestHTTP3ModeUsesSameDefaultMiddlewares(t *testing.T) {
	cfg := regressCfg("127.0.0.1:0")
	cfg.Server.Protocol = "http3"
	log, _ := bufLogger()
	s := New(cfg, log)
	s.router.GET("/", func(c *hypcontext.Context) { c.String(200, "ok") })
	s.applyDefaultMiddlewares()

	for i := 0; i < 200; i++ { // 超過舊限流的 burst（50）
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		if w.Code != 200 {
			t.Fatalf("request %d: status = %d (rate limited by default?)", i, w.Code)
		}
		if i == 0 && w.Header().Get("X-Frame-Options") != "DENY" {
			t.Error("security headers missing in http3 mode")
		}
	}
}

// TestInvalidTrustedProxiesFailsStart 迴歸測試：trusted_proxies 有一筆無效即啟動失敗。
// 歷史 bug：只記一行警告、整份清單作廢，ClientIP 變成代理的位址
func TestInvalidTrustedProxiesFailsStart(t *testing.T) {
	t.Chdir(t.TempDir())
	defer hypcontext.SetTrustedProxies()
	cfg := regressCfg("127.0.0.1:0")
	cfg.Server.TrustedProxies = []string{"127.0.0.1", "10.0.0.0/33"}
	err := startExpectingError(t, New(cfg, logger.NewLogger()))
	if err == nil || !strings.Contains(err.Error(), "trusted_proxies") {
		t.Fatalf("Start = %v, want a trusted_proxies error", err)
	}
}

// TestMaxHandlersQueueTimesOut 迴歸測試：名額用完時等待逾時回 503 並記錄。
// 歷史 bug：無限期等待且沒有任何紀錄，連 /health 都不回應
func TestMaxHandlersQueueTimesOut(t *testing.T) {
	old := handlerQueueTimeout
	handlerQueueTimeout = 200 * time.Millisecond
	defer func() { handlerQueueTimeout = old }()

	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	cfg.Server.MaxHandlers = 1
	log, buf := bufLogger()
	s := New(cfg, log)
	release := make(chan struct{})
	s.router.GET("/hold", func(c *hypcontext.Context) { <-release; c.String(200, "ok") })
	s.router.GET("/health", func(c *hypcontext.Context) { c.String(200, "ok") })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())
	defer close(release)

	go http.Get(fmt.Sprintf("http://127.0.0.1:%d/hold", port))
	time.Sleep(100 * time.Millisecond)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("queued request got no response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") == "" {
		t.Errorf("status = %d, Retry-After = %q; want 503 with Retry-After", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if !strings.Contains(buf.String(), "max_handlers reached") {
		t.Error("rejection was not logged")
	}
}

// TestSecondSignalForcesClose 迴歸測試：關閉期間再收到一次終止訊號即強制中斷。
// 歷史 bug：Shutdown 返回前訊號被攔截而沒人讀，有長連線時只能等滿逾時
func TestSecondSignalForcesClose(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	s := New(cfg, logger.NewLogger())
	s.router.GET("/slow", func(c *hypcontext.Context) {
		time.Sleep(5 * time.Second)
		c.String(200, "done")
	})
	done := startServer(t, s, port)
	go http.Get(fmt.Sprintf("http://127.0.0.1:%d/slow", port))
	time.Sleep(150 * time.Millisecond)

	quit := make(chan os.Signal, 2)
	go s.awaitShutdownSignals(quit)
	start := time.Now()
	quit <- os.Interrupt
	time.Sleep(200 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("first signal should wait for the in-flight request")
	default:
	}
	quit <- os.Interrupt
	select {
	case <-done:
		if el := time.Since(start); el > 2*time.Second {
			t.Errorf("forced close took %v", el)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second signal did not force the shutdown")
	}
}

// TestShutdownTimeoutFromConfig 迴歸測試：關閉期限取自 server.shutdown_timeout（先前固定 30 秒）
func TestShutdownTimeoutFromConfig(t *testing.T) {
	remaining := func(cfg *config.Config) time.Duration {
		ctx, cancel := New(cfg, logger.NewLogger()).shutdownContext()
		defer cancel()
		d, _ := ctx.Deadline()
		return time.Until(d).Round(time.Second)
	}
	if got := remaining(&config.Config{}); got != 30*time.Second {
		t.Errorf("Config{} = %v, want 30s", got)
	}
	cfg := &config.Config{}
	cfg.Server.ShutdownTimeout = 5
	if got := remaining(cfg); got != 5*time.Second {
		t.Errorf("shutdown_timeout 5 = %v", got)
	}
	t.Setenv("HYPGO_SERVER_SHUTDOWN_TIMEOUT", "0")
	cfg = &config.Config{}
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
	cfg.ApplyDefaults()
	if got := remaining(cfg); got != 0 {
		t.Errorf("explicit 0 = %v, want no grace period", got)
	}
}

// TestHeaderTimeoutSurvivesReadTimeoutOff 迴歸測試：read_timeout 關閉時標頭仍有讀取上限。
// 歷史 bug：ReadHeaderTimeout 跟著變成 0（slowloris）
func TestHeaderTimeoutSurvivesReadTimeoutOff(t *testing.T) {
	cfg := &config.Config{}
	srv := New(cfg, logger.NewLogger()).newHTTPServer()
	if srv.ReadTimeout != 0 || srv.ReadHeaderTimeout != headerTimeout {
		t.Errorf("ReadTimeout = %v, ReadHeaderTimeout = %v", srv.ReadTimeout, srv.ReadHeaderTimeout)
	}
	cfg.Server.ReadTimeout = 3
	if srv = New(cfg, logger.NewLogger()).newHTTPServer(); srv.ReadHeaderTimeout != 3*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, want 3s", srv.ReadHeaderTimeout)
	}
}

// TestAltSvcPortAndWithdrawal 迴歸測試：Alt-Svc 可指定對外的埠，HTTP/3 停止服務後不再廣告。
// 歷史 bug：埠一律取本機 UDP 埠（經埠對映時是錯的）；Serve 中途結束仍繼續廣告
func TestAltSvcPortAndWithdrawal(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	certFile, keyFile := selfSignedCert(t, dir)
	port := freeUDPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "auto"
	cfg.Server.AltSvcPort = 443
	cfg.Server.TLS.Enabled, cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile = true, certFile, keyFile
	s := New(cfg, logger.NewLogger())
	s.router.GET("/", func(c *hypcontext.Context) { c.String(200, "ok") })
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	wait := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for (s.altSvc.Load() != nil) != want {
			if time.Now().After(deadline) {
				t.Fatalf("altSvc advertised = %v, want %v", !want, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	get := func() string {
		t.Helper()
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
		resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.Header.Get("Alt-Svc")
	}
	wait(true)
	if got := get(); got != `h3=":443"; ma=86400` {
		t.Errorf("Alt-Svc = %q, want port 443", got)
	}
	s.h3Server.Load().Close() // HTTP/3 中途停止
	wait(false)
	if got := get(); got != "" {
		t.Errorf("Alt-Svc = %q after HTTP/3 stopped", got)
	}
}

// TestProtocolNormalization 迴歸測試：協議名稱不分大小寫，認不得的值啟動失敗。
// 歷史 bug：程式內建構的 Config 寫 "HTTP2" 等會靜默當成 HTTP/1.1
func TestProtocolNormalization(t *testing.T) {
	for in, want := range map[string]string{"": "http1", "HTTP2": "http2", " h3 ": "http3", "Auto": "auto", "h1": "http1"} {
		cfg := &config.Config{}
		cfg.Server.Protocol = in
		if got := New(cfg, logger.NewLogger()).protocol(); got != want {
			t.Errorf("protocol(%q) = %q, want %q", in, got, want)
		}
	}
	t.Chdir(t.TempDir())
	cfg := regressCfg("127.0.0.1:0")
	cfg.Server.Protocol = "spdy"
	if err := startExpectingError(t, New(cfg, logger.NewLogger())); err == nil || !strings.Contains(err.Error(), "spdy") {
		t.Errorf("Start with an unknown protocol = %v", err)
	}
}

// TestStreamIgnoresWriteTimeout 迴歸測試：c.Stream 不受 write_timeout 限制。
// 歷史 bug：串流超過 write_timeout（預設 30 秒）即被切斷，handler 與日誌都看不出來
func TestStreamIgnoresWriteTimeout(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	cfg.Server.WriteTimeout = 1
	s := New(cfg, logger.NewLogger())
	s.router.GET("/stream", func(c *hypcontext.Context) {
		n := 0
		c.Stream(func(w io.Writer) bool {
			fmt.Fprintf(w, "data: %d\n\n", n)
			n++
			time.Sleep(300 * time.Millisecond)
			return n < 6
		})
	})
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/stream", port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if got := strings.Count(string(body), "data:"); err != nil || got != 6 {
		t.Errorf("received %d of 6 chunks, err = %v", got, err)
	}
}

// TestMaxBodySizeIsEnforced 迴歸測試：server.max_body_size 套用到每個請求，超過時 BindInput 回 413；
// middleware.BodyLimit 可以為個別路由放寬。歷史 bug：沒有任何預設上限
func TestMaxBodySizeIsEnforced(t *testing.T) {
	t.Chdir(t.TempDir())
	port := freeTCPPort(t)
	cfg := regressCfg(fmt.Sprintf("127.0.0.1:%d", port))
	cfg.Server.Protocol = "http1"
	cfg.Server.MaxBodySize = "1KB"
	s := New(cfg, logger.NewLogger())
	type req struct {
		Name string `json:"name"`
	}
	h := func(c *hypcontext.Context) {
		var v req
		if c.BindInput(&v) {
			c.JSON(200, len(v.Name))
		}
	}
	s.router.POST("/u", h)
	s.router.POST("/upload", middleware.BodyLimit(middleware.BodyLimitConfig{MaxBytes: 1 << 20}), h)
	startServer(t, s, port)
	defer s.Shutdown(context.Background())

	send := func(path string, size int) int {
		t.Helper()
		body := strings.NewReader(`{"name":"` + strings.Repeat("a", size) + `"}`)
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d%s", port, path), "application/json", body)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := send("/u", 100); got != 200 {
		t.Errorf("small body: %d", got)
	}
	if got := send("/u", 5000); got != http.StatusRequestEntityTooLarge {
		t.Errorf("5KB body with a 1KB limit: %d, want 413", got)
	}
	if got := send("/upload", 5000); got != 200 {
		t.Errorf("route with a raised limit: %d", got)
	}
	// 程式內建構的 Config 沒有設定時用預設的 10MB，不是不限
	if got := (&config.Config{}).Server.MaxBodyBytes(); got != 10<<20 {
		t.Errorf("default MaxBodyBytes = %d", got)
	}
}
