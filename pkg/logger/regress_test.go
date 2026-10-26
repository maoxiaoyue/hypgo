package logger

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// waitFor 輪詢 cond 直到成立或逾時（輪轉後的壓縮 / 清理在背景 goroutine 進行）
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// backupFiles 列出 dir 內 app.log 的時間戳記備份（含 .gz），不含 app.log 與其他旁檔
func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^app-\d{4}-?\d{2}-?\d{2}[-T]\d{2}-?\d{2}-?\d{2}\.\d{3}\.log(\.gz)?$`)
	var out []string
	for _, e := range entries {
		if re.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestRotationRecoversAfterRenameFailure 迴歸測試（A1）：輪轉時 rename 失敗，之後的寫入必須恢復。
// 歷史 bug：rotateLocked 先關檔再 rename，rename 失敗（Windows 上有其他程式開著 app.log）時
// r.file 停在已關閉的檔案上，之後每次寫入都回 "file already closed"，永遠寫不進去。
func TestRotationRecoversAfterRenameFailure(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	chunk := []byte(strings.Repeat("x", 600<<10) + "\n")
	if _, err := r.Write(chunk); err != nil {
		t.Fatal(err)
	}

	// 另一個 handle 開著檔案：Windows 上 rename 會失敗（其他平台照常輪轉）
	reader, err := os.Open(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Write(chunk); err != nil { // 觸發輪轉；Windows 上預期失敗
		t.Logf("rotation while the file is held open: %v", err)
	}
	reader.Close()

	if _, err := r.Write([]byte("after release\n")); err != nil {
		t.Fatalf("write after the reader released the file must succeed, got %v", err)
	}
	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("after release")) {
		t.Errorf("app.log should contain the line written after recovery")
	}
}

// TestNoColorCodesInFileOrPipe 迴歸測試（A2）：寫檔或非終端機輸出時不得含 ANSI 色碼。
// 歷史 bug：只檢查 l.file == nil，而 NewWithRotation 從不設 l.file——colors: true 的
// hyp api 樣板（output: logs/api.log）把色碼寫進檔案；Docker / 管線輸出同樣夾帶色碼。
func TestNoColorCodesInFileOrPipe(t *testing.T) {
	logfile := filepath.Join(t.TempDir(), "app.log")
	l, err := NewWithRotation("info", logfile, &RotationConfig{}, true)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("to file", "k", "v")
	l.Close()
	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("\x1b[")) {
		t.Errorf("log file must not contain ANSI escapes: %q", data)
	}

	var buf bytes.Buffer
	l2, _ := New("info", "", &buf, true)
	l2.Info("to pipe")
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("non-terminal writer must not receive ANSI escapes: %q", buf.String())
	}
}

// TestCleanupKeepsSiblingLogs 迴歸測試（A3）：清理舊備份不得刪掉同前綴的其他日誌。
// 歷史 bug：cleanup 以 prefix+"-*" glob，app.log 輪轉時把 app-access.log、app-error.log 一起刪掉。
func TestCleanupKeepsSiblingLogs(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	for _, name := range []string{"app-access.log", "app-error.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("keep\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := NewRotation(logfile, &RotationConfig{MaxBackups: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < 3; i++ {
		if _, err := r.Write([]byte("line\n")); err != nil {
			t.Fatal(err)
		}
		if err := r.Rotate(); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(3*time.Second, func() bool { return len(backupFiles(t, dir)) <= 1 })
	time.Sleep(200 * time.Millisecond) // 讓背景清理跑完
	for _, name := range []string{"app-access.log", "app-error.log"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s must survive backup cleanup: %v", name, err)
		}
	}
	if got := backupFiles(t, dir); len(got) != 1 {
		t.Errorf("expected exactly max_backups=1 backup, got %v", got)
	}
}

// TestCompressReplacesBackup 迴歸測試（A4）：壓縮後只留 .gz，內容完整。
// 歷史 bug：Windows 上在 defer Close 之前 os.Remove 原檔，刪除失敗而兩份並存；
// gz.Close 的錯誤也被忽略，壓縮檔可能殘缺。
func TestCompressReplacesBackup(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	r, err := NewRotation(logfile, &RotationConfig{Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	payload := strings.Repeat("compress me\n", 100)
	if _, err := r.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	if err := r.Rotate(); err != nil {
		t.Fatal(err)
	}

	ok := waitFor(3*time.Second, func() bool {
		files := backupFiles(t, dir)
		return len(files) == 1 && strings.HasSuffix(files[0], ".gz")
	})
	if !ok {
		t.Fatalf("expected the backup to be replaced by a single .gz, got %v", backupFiles(t, dir))
	}
	f, err := os.Open(filepath.Join(dir, backupFiles(t, dir)[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("compressed backup is incomplete: %v", err)
	}
	if string(got) != payload {
		t.Errorf("decompressed backup = %d bytes, want %d", len(got), len(payload))
	}
}

// TestSetLevelConcurrentWithLogging 迴歸測試（A5）：SetLevel 與並行寫入不得有 data race（需 -race）。
// 歷史 bug：log()/logf() 在鎖外讀 l.level，SetLevel 在鎖內寫，race detector 回報競爭。
func TestSetLevelConcurrentWithLogging(t *testing.T) {
	l, _ := New("info", "", io.Discard, false)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.Info("tick", "j", j)
				l.Debugf("tick %d", j)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				l.SetLevel(Level(j % 6))
			}
		}()
	}
	wg.Wait()
}

// TestTextOutputEscapesInjection 迴歸測試（A6）：值或訊息含換行 / 空白時不得偽造日誌行或欄位。
// 歷史 bug：文字模式原樣寫出 msg、key、value，"alice\n[ERROR] forged" 會變成另一行 ERROR 日誌，
// "a b" 也無法和下一個欄位區分。
func TestTextOutputEscapesInjection(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("info", "", &buf, false)

	l.Info("login", "user", "alice\n[ERROR] forged", "note", "a b", "eq", "x=y", "empty", "")
	l.Info("evil\n[ERROR] forged message")

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines, got %d: %q", len(lines), buf.String())
	}
	for _, want := range []string{`user="alice\n[ERROR] forged"`, `note="a b"`, `eq="x=y"`, `empty=""`} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line 1 should contain %s, got %q", want, lines[0])
		}
	}
	if !strings.Contains(lines[1], `"evil\n[ERROR] forged message"`) {
		t.Errorf("message with a newline should be quoted, got %q", lines[1])
	}
}

// TestJSONCustomLevelNames 迴歸測試（B）：JSON 模式的 NOTICE / EMERGENCY 層級名稱。
// 歷史 bug：直接沿用 slog 預設命名，輸出 "INFO+2" / "ERROR+4"。
func TestJSONCustomLevelNames(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("debug", "", &buf, false)
	l.SetFormat("json")
	l.Notice("n")
	l.Emergencyf("e %d", 1)
	l.Warn("w")

	out := buf.String()
	for _, want := range []string{`"level":"NOTICE"`, `"level":"EMERGENCY"`, `"level":"WARN"`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON output should contain %s, got %s", want, out)
		}
	}
}

// TestNewWithFilePathOutput 迴歸測試（B）：New 的 output 為檔案路徑時寫入該檔案。
// 歷史 bug：非 "stdout" 的 output 一律靜默改寫到 stderr，desktop / gRPC 樣板
// 以 logger.New(cfg.Logger.Level, cfg.Logger.Output, ...) 設定的日誌檔從未產生。
func TestNewWithFilePathOutput(t *testing.T) {
	logfile := filepath.Join(t.TempDir(), "logs", "desktop.log")
	l, err := New("info", logfile, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello file")
	l.Close()

	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatalf("log file should exist: %v", err)
	}
	if !strings.Contains(string(data), "hello file") {
		t.Errorf("log file should contain the message, got %q", data)
	}
}

// TestCloseDropsLaterWrites 迴歸測試（B）：Close 可重複呼叫，之後的日誌被丟棄而非重新開檔。
// 歷史 bug：Close 後 l.out 仍指向輪轉 writer，下一筆日誌讓 Rotation.Write 把檔案重新打開。
func TestCloseDropsLaterWrites(t *testing.T) {
	logfile := filepath.Join(t.TempDir(), "app.log")
	l, err := NewWithRotation("info", logfile, &RotationConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	l.Info("before close")
	l.Close()
	l.Info("after close")
	l.Close()

	data, err := os.ReadFile(logfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "before close") || strings.Contains(string(data), "after close") {
		t.Errorf("file should contain only the line logged before Close, got %q", data)
	}
}

// TestRapidRotationsKeepEveryBackup 迴歸測試（B）：連續快速輪轉不得覆蓋前一份備份。
// 歷史 bug：備份檔名只精確到毫秒，同一毫秒內兩次輪轉時 rename 覆蓋上一份備份，日誌遺失。
func TestRapidRotationsKeepEveryBackup(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRotation(filepath.Join(dir, "app.log"), &RotationConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	const n = 20
	for i := 0; i < n; i++ {
		if _, err := r.Write([]byte("entry\n")); err != nil {
			t.Fatal(err)
		}
		if err := r.Rotate(); err != nil {
			t.Fatal(err)
		}
	}
	if got := backupFiles(t, dir); len(got) != n {
		t.Errorf("expected %d distinct backups, got %d", n, len(got))
	}
}

// TestOversizedLineNoEmptyBackup 迴歸測試（B）：單行超過 max_size 時不得切出空備份。
// 歷史 bug：新檔第一筆就超過門檻時照樣先輪轉，留下 0 byte 的備份檔。
func TestOversizedLineNoEmptyBackup(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if _, err := r.Write([]byte(strings.Repeat("y", 2<<20) + "\n")); err != nil {
		t.Fatal(err)
	}
	for _, name := range backupFiles(t, dir) {
		if fi, err := os.Stat(filepath.Join(dir, name)); err == nil && fi.Size() == 0 {
			t.Errorf("empty backup %s created for an oversized line", name)
		}
	}
	if fi, err := os.Stat(logfile); err != nil || fi.Size() == 0 {
		t.Errorf("app.log should hold the (truncated) line: %v", err)
	}
}

// TestRotationSizeParsing 迴歸測試（B）：max_size 接受常見寫法，拒絕負數與亂碼。
// 歷史 bug：parseSize 只認 KB/MB/GB 整數，"100M"、"1.5GB" 被拒；"-5MB" 反而通過。
func TestRotationSizeParsing(t *testing.T) {
	for _, ok := range []string{"100M", "100MB", "1.5GB", "512KB", "1048576"} {
		r, err := NewRotation(filepath.Join(t.TempDir(), "app.log"), &RotationConfig{MaxSize: ok})
		if err != nil {
			t.Errorf("max_size %q should be accepted: %v", ok, err)
			continue
		}
		r.Close()
	}
	for _, bad := range []string{"-5MB", "abc", "5XB", "1e3"} {
		if r, err := NewRotation(filepath.Join(t.TempDir(), "app.log"), &RotationConfig{MaxSize: bad}); err == nil {
			r.Close()
			t.Errorf("max_size %q should be rejected", bad)
		}
	}
}

// TestSetFileOnNilLogger 迴歸測試（B）：nil Logger 呼叫 SetFile 回傳錯誤而非 panic。
// 歷史 bug：SetFile 沒有 nil 檢查（其他 setter 都有），直接 nil pointer dereference。
func TestSetFileOnNilLogger(t *testing.T) {
	var l *Logger
	if err := l.SetFile(filepath.Join(t.TempDir(), "x.log")); err == nil {
		t.Error("SetFile on a nil Logger should return an error")
	}
}

// TestSetFileFailureKeepsOutput 迴歸測試（B）：SetFile 開檔失敗時保留原輸出。
// 歷史 bug：先關掉舊檔再開新檔，開檔失敗後輸出停在已關閉的檔案上，之後日誌全部消失。
func TestSetFileFailureKeepsOutput(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.log")
	l, _ := New("info", "", io.Discard, false)
	if err := l.SetFile(good); err != nil {
		t.Fatal(err)
	}
	if err := l.SetFile(dir); err == nil { // 目錄無法當檔案開啟
		t.Fatal("SetFile on a directory should fail")
	}
	l.Info("still logging")
	l.Close()

	data, _ := os.ReadFile(good)
	if !strings.Contains(string(data), "still logging") {
		t.Errorf("previous file should keep receiving logs after a failed SetFile, got %q", data)
	}
}

// TestMaxAgeIsRetention 迴歸測試（B）：max_age 為備份保留期限，不是輪轉間隔。
// 歷史 bug：max_age 被當成「自行程啟動起每隔多久輪轉一次」，過期備份從不刪除，
// 日誌目錄只靠 max_backups 控制。
func TestMaxAgeIsRetention(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "app-2000-01-01T00-00-00.000.log")
	if err := os.WriteFile(stale, []byte("old\n"), 0644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRotation(filepath.Join(dir, "app.log"), &RotationConfig{MaxAge: "7d"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Write([]byte("now\n")); err != nil {
		t.Fatal(err)
	}
	if err := r.Rotate(); err != nil {
		t.Fatal(err)
	}

	gone := waitFor(3*time.Second, func() bool {
		_, err := os.Stat(stale)
		return os.IsNotExist(err)
	})
	if !gone {
		t.Error("backup older than max_age should be deleted")
	}
}

// TestTextTimestampPrecision 迴歸測試（B）：文字模式時間戳記含毫秒與時區。
// 歷史 bug：沿用 log.LstdFlags，只到秒且沒有時區，跨主機 / 同秒多筆無法排序。
func TestTextTimestampPrecision(t *testing.T) {
	var buf bytes.Buffer
	l, _ := New("info", "", &buf, false)
	l.Info("ts")
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}(Z|[+-]\d{2}:\d{2}) \[INFO\] ts\n$`)
	if !re.MatchString(buf.String()) {
		t.Errorf("unexpected text line: %q", buf.String())
	}
}
