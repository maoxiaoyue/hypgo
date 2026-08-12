package server

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

func TestNewServer(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	log := logger.NewLogger()

	s := New(&cfg, log)

	if s == nil {
		t.Errorf("New() returned nil")
	}
	if s.config != &cfg {
		t.Errorf("Config pointer mismatch")
	}
	if s.logger != log {
		t.Errorf("Logger pointer mismatch")
	}
	if s.router == nil {
		t.Errorf("Router not initialized")
	}
	if s.sessionCache == nil {
		t.Errorf("SessionCache not initialized")
	}
}

func TestServerRouter(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	r := s.Router()
	if r == nil {
		t.Errorf("Router() returned nil")
	}
	if r != s.router {
		t.Errorf("Router returned is not the same instance")
	}
}

func TestServerHealth(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// test health before starting
	err := s.Health()
	if err == nil {
		t.Errorf("Expected error from Health when not started")
	}

	// Fake start
	s.httpServer.Store(&http.Server{})
	if err := s.Health(); err != nil {
		t.Errorf("Expected no error from Health when started, got: %v", err)
	}

	// Simulate shutdown
	s.shuttingDown.Store(true)
	if err := s.Health(); err == nil {
		t.Errorf("Expected error from Health when shutting down")
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	go func() {
		err := s.Start()
		if err != nil && err != http.ErrServerClosed {
			t.Logf("Start() returned: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := s.Shutdown(ctx)
	if err != nil {
		t.Logf("Shutdown returned: %v", err)
	}

	// 確認 shuttingDown 已設置
	if !s.shuttingDown.Load() {
		t.Error("shuttingDown should be true after Shutdown")
	}
}

// TestStartReturnsNilOnGracefulShutdown 回歸測試：優雅關閉時 Start() 應回傳 nil。
// 歷史 bug：Serve/ListenAndServe 回傳的 http.ErrServerClosed（預期流程）
// 未被過濾、原樣傳給呼叫端，導致 main.go 在每次正常關閉時誤報 Server error。
func TestStartReturnsNilOnGracefulShutdown(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start() }()

	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start() after graceful shutdown = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start() did not return after Shutdown")
	}
}

// TestShutdownIdempotent 回歸測試：Shutdown 必須冪等。
// 歷史 bug：二次呼叫 close 已關閉的 shutdownChan → panic。
func TestShutdownIdempotent(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	// 第二次呼叫不得 panic
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("second Shutdown should return nil, got %v", err)
	}
}

// TestGracefulRestartDisabledOnWindows Windows 一律停用優雅重啟：
// FD 3 繼承慣例不成立，重啟必以子行程 bind 失敗、服務消失收場，
// 且 os.Interrupt 重啟訊號會劫持 Ctrl+C 的關閉語意。
func TestGracefulRestartDisabledOnWindows(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.EnableGracefulRestart = true
	s := New(&cfg, logger.NewLogger())

	want := runtime.GOOS != "windows"
	if got := s.isGracefulRestartEnabled(); got != want {
		t.Errorf("isGracefulRestartEnabled() on %s = %v, want %v", runtime.GOOS, got, want)
	}
}

// TestStartAppliesDefaultMiddlewares 回歸測試：Start() 必須套用預設中間件。
// 歷史 bug：applyDefaultMiddlewares 只被零 caller 的
// ListenAndServeWithGracefulShutdown 呼叫——主要入口跑在無 Recovery、
// 無安全頭的狀態；handler panic 時客戶端拿不到 500。
func TestStartAppliesDefaultMiddlewares(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	s.Router().GET("/ok", func(c *hypcontext.Context) { c.String(200, "ok") })
	s.Router().GET("/boom", func(c *hypcontext.Context) { panic("kaboom") })

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start() }()

	// 等待 listener 就緒
	var addr string
	for i := 0; i < 50; i++ {
		if ln := s.getListenerAtomic(); ln != nil {
			addr = ln.Addr().String()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("listener not ready within timeout")
	}

	// Security 中間件的安全頭應存在（證明預設中間件已套用）
	resp, err := http.Get("http://" + addr + "/ok")
	if err != nil {
		t.Fatalf("GET /ok: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY (default Security middleware not applied?)", got)
	}

	// Recovery 中間件應把 handler panic 轉為 500（同時驗證 gin 式執行模型）
	resp2, err := http.Get("http://" + addr + "/boom")
	if err != nil {
		t.Fatalf("GET /boom: %v (panic escaped Recovery?)", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Errorf("GET /boom status = %d, want 500 (Recovery not working)", resp2.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start() = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start() did not return after Shutdown")
	}
}

func TestProtocolGetter(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	s.protocol = HTTP1
	if p := s.GetProtocol(); p != "HTTP/1.1" {
		t.Errorf("Expected HTTP/1.1, got %s", p)
	}

	s.protocol = HTTP2
	if p := s.GetProtocol(); p != "HTTP/2" {
		t.Errorf("Expected HTTP/2, got %s", p)
	}

	s.protocol = HTTP3
	if p := s.GetProtocol(); p != "HTTP/3" {
		t.Errorf("Expected HTTP/3, got %s", p)
	}
}

// --- SessionCache 測試 ---

func TestSessionCachePutAndGetAndDelete(t *testing.T) {
	sc := newSessionCache()

	sc.Put("key1", []byte("data1"))
	if sc.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", sc.Len())
	}

	data, ok := sc.GetAndDelete("key1")
	if !ok {
		t.Fatal("GetAndDelete returned false")
	}
	if string(data) != "data1" {
		t.Errorf("data = %q, want %q", data, "data1")
	}

	// 第二次取應該不存在（已刪除）
	_, ok = sc.GetAndDelete("key1")
	if ok {
		t.Error("GetAndDelete should return false on second call (replay prevention)")
	}
}

func TestSessionCacheMaxSize(t *testing.T) {
	sc := &SessionCache{
		entries: make(map[string]sessionEntry),
		maxSize: 3,
		ttl:     time.Hour,
	}

	sc.Put("a", []byte("1"))
	sc.Put("b", []byte("2"))
	sc.Put("c", []byte("3"))
	sc.Put("d", []byte("4")) // 應淘汰最舊

	if sc.Len() > 3 {
		t.Errorf("Len() = %d, want <= 3 (should evict)", sc.Len())
	}
}

func TestSessionCacheTTLExpiry(t *testing.T) {
	sc := &SessionCache{
		entries: make(map[string]sessionEntry),
		maxSize: 100,
		ttl:     50 * time.Millisecond,
	}

	sc.Put("expired", []byte("data"))
	time.Sleep(100 * time.Millisecond)

	_, ok := sc.GetAndDelete("expired")
	if ok {
		t.Error("expired session should not be returned")
	}
}

func TestSessionCacheConcurrency(t *testing.T) {
	sc := newSessionCache()
	var wg sync.WaitGroup

	// 並行寫入
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key" + string(rune('0'+n%10))
			sc.Put(key, []byte("data"))
		}(i)
	}

	// 並行讀取
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key" + string(rune('0'+n%10))
			sc.GetAndDelete(key)
		}(i)
	}

	wg.Wait()
	// 無 race condition panic 即通過
}

// --- NotFound / MethodNotAllowed 測試 ---

func TestServerNotFound(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	called := false
	s.NotFound(func(c *hypcontext.Context) {
		called = true
		c.Status(404)
		c.Writer.WriteHeaderNow()
	})

	// 驗證不會 panic（之前是空殼）
	if called {
		t.Error("handler should not be called yet (only registered)")
	}
}

// --- loadCertificate 測試 ---

func TestLoadCertificateError(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 不存在的證書路徑
	s.config.Server.TLS.CertFile = "/nonexistent/cert.pem"
	s.config.Server.TLS.KeyFile = "/nonexistent/key.pem"

	_, err := s.loadCertificate()
	if err == nil {
		t.Error("loadCertificate should return error for missing cert files")
	}
}

// --- Shutdown atomic 測試 ---

func TestShutdownAtomic(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 初始狀態
	if s.shuttingDown.Load() {
		t.Error("shuttingDown should be false initially")
	}

	// 並行讀寫不應 race
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Health()
		}()
	}

	s.shuttingDown.Store(true)
	wg.Wait()

	if !s.shuttingDown.Load() {
		t.Error("shuttingDown should be true after Store(true)")
	}
}

// --- #5: waitForChildAlive 真輪詢測試 ---

// TestWaitForChildAliveDetectsDeadProcess 回歸測試：子行程提前結束時必須提早偵測到，
// 不能像舊版一樣不管三七二十一空睡滿整個時長（15 次 × 200ms = 3 秒）。
func TestWaitForChildAliveDetectsDeadProcess(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 產生一個立刻結束的子行程（執行自己但不跑任何測試）
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // 給它時間結束

	start := time.Now()
	alive := s.waitForChildAlive(cmd.Process, 15, 200*time.Millisecond)
	elapsed := time.Since(start)

	if alive {
		t.Error("waitForChildAlive should report false for an already-exited process")
	}
	if elapsed > 1*time.Second {
		t.Errorf("waitForChildAlive took %v to detect a dead process, want early exit (~200ms), not the full 3s window", elapsed)
	}
}

// TestWaitForChildAliveDetectsLiveProcess 子行程全程存活時應回傳 true。
// Windows 上 (*os.Process).Signal 僅支援 os.Kill/os.Interrupt，
// 存活探測必定回錯——這正是 graceful restart 在 Windows 停用的原因，故跳過。
func TestWaitForChildAliveDetectsLiveProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liveness probe via Signal(0) is unsupported on Windows (graceful restart is Windows-disabled)")
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn helper process: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	if !s.waitForChildAlive(cmd.Process, 3, 50*time.Millisecond) {
		t.Error("waitForChildAlive should report true for a process alive throughout the poll window")
	}
}

// --- #6: Health()/Start() 無資料競態測試 ---

// TestHealthNoRaceWithStart 回歸測試：Health() 讀取 httpServer/h3Server 與 Start()
// 的寫入之間不得 race（改用 atomic.Pointer 後應無競態）。用 `go test -race` 執行時最具意義。
func TestHealthNoRaceWithStart(t *testing.T) {
	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Server.Addr = "127.0.0.1:0"
	s := New(&cfg, logger.NewLogger())

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = s.Start()
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			_ = s.Health()
			time.Sleep(time.Millisecond)
		}
	}()

	time.Sleep(150 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	wg.Wait()
}

// --- #7: PID 檔多實例衝突測試 ---

// TestSavePIDFileWarnsOnForeignAlivePID 回歸測試：既有 PID 檔屬於另一個仍存活的行程時
// （同機同 CWD 誤起兩個 hypgo 實例），必須先警告才覆寫，不能靜默覆蓋。
func TestSavePIDFileWarnsOnForeignAlivePID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("liveness probe via Signal(0) is unsupported on Windows")
	}
	dir := t.TempDir()
	t.Chdir(dir)

	// 起一個目前仍存活、PID 與本測試行程不同的子行程，模擬「另一個 hypgo 實例」
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn helper process: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	if err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(cmd.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	log, _ := logger.New("debug", "", &buf, false)
	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, log)

	if err := s.savePIDFile(); err != nil {
		t.Fatalf("savePIDFile: %v", err)
	}

	if !strings.Contains(buf.String(), "already belongs to running PID") {
		t.Errorf("expected a collision warning in log output, got: %s", buf.String())
	}

	data, _ := os.ReadFile(pidFilePath)
	if got := strings.TrimSpace(string(data)); got != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file = %q, want %d (own pid, overwritten)", got, os.Getpid())
	}
}

// TestRemovePIDFileOnlyRemovesOwnPID 回歸測試：removePIDFile 只能移除屬於自己的 PID 檔，
// 避免誤刪另一個仍存活實例的 PID 檔。
func TestRemovePIDFileOnlyRemovesOwnPID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfg := config.Config{}
	cfg.ApplyDefaults()
	s := New(&cfg, logger.NewLogger())

	// 別人的 PID（removePIDFile 只比對數字，不需要真的存活）
	if err := os.WriteFile(pidFilePath, []byte("999999"), 0644); err != nil {
		t.Fatal(err)
	}
	s.removePIDFile()
	if _, err := os.Stat(pidFilePath); err != nil {
		t.Error("removePIDFile should not remove a PID file belonging to a different process")
	}

	// 自己的 PID → 應該被移除
	if err := os.WriteFile(pidFilePath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatal(err)
	}
	s.removePIDFile()
	if _, err := os.Stat(pidFilePath); !os.IsNotExist(err) {
		t.Error("removePIDFile should remove a PID file belonging to this process")
	}
}
