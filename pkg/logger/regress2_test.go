package logger

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"gopkg.in/natefinch/lumberjack.v2"
)

// captureErrOut 把日誌自身的錯誤回報導到緩衝區
func captureErrOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := errOut
	errOut = &buf
	t.Cleanup(func() { errOut = old })
	return &buf
}

// countLines 回傳 dir 內所有檔案的行數總和與檔案數
func countLines(t *testing.T, dir string) (lines, files int) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		lines += bytes.Count(data, []byte("\n"))
		files++
	}
	return
}

// 先前輪轉失敗時那一筆直接丟掉；條件持續多久就丟多久，而且沒有任何提示。
// 以 ljRotate 模擬 rename 失敗（lumberjack 失敗時已把檔案關掉），所有平台都能測
func TestRotationFailureKeepsWriting(t *testing.T) {
	stderr := captureErrOut(t)
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")

	failing, attempts := true, 0
	realRotate := ljRotate
	ljRotate = func(l *lumberjack.Logger) error {
		attempts++
		if failing {
			l.Close()
			return errors.New("rename blocked")
		}
		return realRotate(l)
	}
	defer func(d time.Duration) { ljRotate, rotateRetry = realRotate, d }(rotateRetry)
	rotateRetry = time.Hour // 這段期間只該試一次

	l, err := NewWithRotation("info", logfile, &RotationConfig{MaxSize: "1MB", MaxBackups: 5}, false)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("z", 1000)
	const total = 3000 // 約 3.1MB
	for i := 0; i < total; i++ {
		l.Info(line, "i", i)
	}
	if lines, files := countLines(t, dir); lines != total || files != 1 {
		t.Fatalf("while rotation fails: %d lines in %d file(s), want %d lines in 1 file", lines, files, total)
	}
	if attempts != 1 {
		t.Errorf("rotation attempted %d times within rotateRetry, want 1", attempts)
	}
	if n := strings.Count(stderr.String(), "cannot rotate"); n != 1 || !strings.Contains(stderr.String(), "rename blocked") {
		t.Errorf("stderr should report the failure once: %q", stderr.String())
	}

	// 可以輪轉了：下一筆就切檔並回報恢復，之後不再繞過 lumberjack
	failing, rotateRetry = false, 0
	l.Info(line, "after", 1)
	l.Info(line, "after", 2)
	l.Close()
	if lines, files := countLines(t, dir); lines != total+2 || files != 2 {
		t.Errorf("after recovery: %d lines in %d file(s), want %d lines in 2 files", lines, files, total+2)
	}
	if info, _ := os.Stat(logfile); info == nil || info.Size() > 4000 {
		t.Errorf("app.log should hold only the lines after the rotation: %v", info)
	}
	if n := strings.Count(stderr.String(), "rotated after failing"); n != 1 {
		t.Errorf("stderr should report the recovery once: %q", stderr.String())
	}
}

// 真實情境：Windows 上另一個程式（tail、編輯器）開著日誌檔，rename 失敗
func TestRotationBlockedByOpenReader(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("rename is only blocked by another open handle on Windows")
	}
	captureErrOut(t)
	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	l, err := NewWithRotation("info", logfile, &RotationConfig{MaxSize: "1MB"}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	l.Info("start")
	reader, err := os.Open(logfile)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("z", 1000)
	for i := 0; i < 2500; i++ {
		l.Info(line)
	}
	reader.Close()
	if lines, _ := countLines(t, dir); lines != 2501 {
		t.Errorf("%d of 2501 lines on disk: lines written while the file was held open were lost", lines)
	}
}

// 明確呼叫 Rotate 成功也算恢復
func TestExplicitRotateClearsBlockedState(t *testing.T) {
	stderr := captureErrOut(t)
	logfile := filepath.Join(t.TempDir(), "app.log")
	failing := true
	realRotate := ljRotate
	ljRotate = func(l *lumberjack.Logger) error {
		if failing {
			l.Close()
			return errors.New("rename blocked")
		}
		return realRotate(l)
	}
	defer func() { ljRotate = realRotate }()

	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	chunk := []byte(strings.Repeat("x", 600<<10) + "\n")
	r.Write(chunk)
	r.Write(chunk) // 觸發輪轉、失敗
	if !r.blocked {
		t.Fatal("rotation failure not recorded")
	}
	failing = false
	if err := r.Rotate(); err != nil {
		t.Fatal(err)
	}
	if r.blocked || !strings.Contains(stderr.String(), "rotated after failing") {
		t.Errorf("blocked=%v stderr=%q", r.blocked, stderr.String())
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

// 先前寫入失敗完全無聲：Error() 沒有回傳值，也沒有任何地方印出來
func TestWriteFailureIsReported(t *testing.T) {
	stderr := captureErrOut(t)
	l, _ := New("info", "", failWriter{errors.New("disk full")}, false)
	for i := 0; i < 50; i++ {
		l.Error("lost")
	}
	l.Slog().Error("lost via slog")
	out := stderr.String()
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "disk full") || !strings.Contains(out, "1 log line(s) lost") {
		t.Fatalf("want one report (rate limited) naming the error: %q", out)
	}

	// 過了間隔再回報一次，帶上這段期間累計的筆數
	l.out.mu.Lock()
	l.out.lastReport = time.Now().Add(-2 * writeErrorInterval)
	l.out.mu.Unlock()
	l.Error("lost again")
	if out := stderr.String(); strings.Count(out, "\n") != 2 || !strings.Contains(out, "51 log line(s) lost") {
		t.Errorf("second report should carry the count since the first: %q", out)
	}
}

// 輸出本身就是 stderr 而寫不進去時，不再往 stderr 回報
func TestWriteFailureOnStderrIsNotReported(t *testing.T) {
	stderr := captureErrOut(t)
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	old := os.Stderr
	os.Stderr = closed
	defer func() { os.Stderr = old }()

	l, _ := New("info", "stderr", nil, false)
	l.Error("cannot be written")
	if stderr.Len() != 0 {
		t.Errorf("reported a stderr failure to stderr: %q", stderr.String())
	}
}

// 先前 "Debug"、" debug"、"trace" 都靜默變成 INFO
func TestLevelIsCaseInsensitiveAndUnknownIsAnError(t *testing.T) {
	for in, want := range map[string]Level{"Debug": DEBUG, " debug ": DEBUG, "WARN": WARNING, "Warning": WARNING, "Emergency": EMERGENCY, "": INFO} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{"trace", "off", "fatal", "err"} {
		if got, err := ParseLevel(in); err == nil || got != INFO || !strings.Contains(err.Error(), in) {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}

	var buf bytes.Buffer
	l, err := New("Debug", "", &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	l.Debug("d")
	if !strings.Contains(buf.String(), "[DEBUG] d") {
		t.Errorf(`New("Debug") did not enable debug: %q`, buf.String())
	}

	// 認不得的層級：回錯，同時仍回傳可用的 INFO logger
	buf.Reset()
	l, err = New("trace", "", &buf, false)
	if err == nil || l == nil {
		t.Fatalf(`New("trace") = %v, %v`, l, err)
	}
	l.Debug("d")
	l.Info("i")
	if got := buf.String(); strings.Contains(got, "[DEBUG]") || !strings.Contains(got, "[INFO] i") {
		t.Errorf("fallback logger should be INFO: %q", got)
	}

	rl, err := NewWithRotation("trace", filepath.Join(t.TempDir(), "a.log"), nil, false)
	if err == nil || rl == nil {
		t.Errorf(`NewWithRotation("trace") = %v, %v`, rl, err)
	}
	rl.Close()
}

// 先前只有 hypgo.New 套用設定檔的 rotation；logger.New 建立的一律是 100MB / 10 份
func TestNewWithOptionsAppliesEverything(t *testing.T) {
	dir := t.TempDir()
	logfile := filepath.Join(dir, "logs", "app.log")
	l, err := NewWithOptions(Options{
		Level: "Debug", Output: " " + logfile + " ", Format: "JSON", Colors: true,
		Rotation: &RotationConfig{MaxSize: "1MB", MaxBackups: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 1000)
	for i := 0; i < 4500; i++ { // 約 4.7MB：1MB 一檔，保留 2 份備份
		l.Debug(line)
	}
	l.Close()
	if !waitFor(3*time.Second, func() bool { return len(backupFiles(t, filepath.Dir(logfile))) == 2 }) {
		t.Errorf("backups = %v, want 2 (rotation settings not applied)", backupFiles(t, filepath.Dir(logfile)))
	}
	data, _ := os.ReadFile(logfile)
	if !bytes.HasPrefix(data, []byte(`{"time":`)) || !bytes.Contains(data, []byte(`"level":"DEBUG"`)) || bytes.Contains(data, []byte("\033[")) {
		t.Errorf("want JSON debug lines without colour codes: %.120q", data)
	}

	// stdout / stderr 不分大小寫、去空白：先前 "STDOUT" 被當成檔名
	t.Chdir(dir)
	for _, out := range []string{"STDOUT", " Stderr ", ""} {
		l, err := NewWithOptions(Options{Output: out})
		if err != nil {
			t.Errorf("Output %q: %v", out, err)
		}
		l.Close()
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a file was created for stdout / stderr: %v", entries)
	}
}

// 設定值無效時回錯，但回傳的 logger 仍可用
func TestNewWithOptionsReportsInvalidSettings(t *testing.T) {
	dir := t.TempDir()
	notDir := filepath.Join(dir, "file")
	os.WriteFile(notDir, nil, 0o644)
	for name, o := range map[string]Options{
		"format":   {Format: "xml"},
		"level":    {Level: "loud"},
		"rotation": {Output: filepath.Join(dir, "a.log"), Rotation: &RotationConfig{MaxSize: "big"}},
		"open":     {Output: filepath.Join(notDir, "sub", "a.log")},
	} {
		l, err := NewWithOptions(o)
		if err == nil || l == nil {
			t.Errorf("%s: logger=%v err=%v", name, l, err)
			continue
		}
		var buf bytes.Buffer
		l.SetOutput(&buf)
		l.Info("still works")
		if !strings.Contains(buf.String(), "[INFO] still works") {
			t.Errorf("%s: fallback logger not usable: %q", name, buf.String())
		}
	}
	// nil Rotation 使用預設值（100MB），不是「不輪轉」
	l, err := NewWithOptions(Options{Output: filepath.Join(dir, "b.log")})
	if err != nil {
		t.Fatal(err)
	}
	if rot, ok := l.out.closer.(*Rotation); !ok || rot.maxBytes != 100*megabyte || rot.lj.MaxBackups != 10 {
		t.Errorf("nil Rotation should mean the defaults: %+v", l.out.closer)
	}
	l.Close()
}

// 先前 "1M" 轉成小寫後被當成 1 分鐘，再進位成保留 1 天
func TestMaxAgeUppercaseMIsRejected(t *testing.T) {
	for _, s := range []string{"1M", " 6M "} {
		if _, err := parseAge(s); err == nil || !strings.Contains(err.Error(), "30d") {
			t.Errorf("parseAge(%q) err = %v", s, err)
		}
	}
	if d, err := parseAge("30m"); err != nil || d != 30*time.Minute {
		t.Errorf("parseAge(30m) = %v, %v", d, err)
	}
	if _, err := NewRotation(filepath.Join(t.TempDir(), "a.log"), &RotationConfig{MaxAge: "1M"}); err == nil {
		t.Error("NewRotation accepted max_age 1M")
	}
}

// 先前超長的一行被截在多位元組字元中間
func TestOversizedLineCutOnRuneBoundary(t *testing.T) {
	logfile := filepath.Join(t.TempDir(), "app.log")
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pad := range []string{"", "a", "ab"} { // 三種對齊，至少一種會落在字元中間
		if n, err := r.Write([]byte(pad + strings.Repeat("字", 500000) + "\n")); err != nil || n == 0 {
			t.Fatal(n, err)
		}
		r.Rotate()
	}
	r.Close()
	entries, _ := os.ReadDir(filepath.Dir(logfile))
	checked := 0
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(filepath.Dir(logfile), e.Name()))
		if len(data) == 0 {
			continue
		}
		checked++
		if !utf8.Valid(data) || data[len(data)-1] != '\n' || len(data) >= megabyte {
			t.Errorf("%s: valid UTF-8=%v, %d bytes, tail=%q", e.Name(), utf8.Valid(data), len(data), data[len(data)-6:])
		}
	}
	if checked != 3 {
		t.Errorf("checked %d files, want 3", checked)
	}
}

// 檔案被別的行程寫到超過上限時，lumberjack 在 Write 裡自行輪轉；Windows 上有人開著檔案時它會失敗。
// 先前這條路徑每一筆都回錯（兩個行程寫同一個檔，其中一個的日誌全部消失）。
// 其他平台 rename 不會失敗，這個測試在那裡只確認沒有掉行
func TestWriteSurvivesLumberjackInternalRotationFailure(t *testing.T) {
	captureErrOut(t)
	defer func(d time.Duration) { rotateRetry = d }(rotateRetry)
	rotateRetry = time.Hour

	dir := t.TempDir()
	logfile := filepath.Join(dir, "app.log")
	r, err := NewRotation(logfile, &RotationConfig{MaxSize: "1MB"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// 另一個行程把檔案寫到 1.5MB，並且一直開著它
	other, err := os.OpenFile(logfile, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	other.WriteString(strings.Repeat(strings.Repeat("o", 1023)+"\n", 1536))
	defer other.Close()

	for i := 0; i < 100; i++ {
		if _, err := r.Write([]byte("mine\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	mine := 0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		mine += bytes.Count(data, []byte("mine\n"))
	}
	if mine != 100 {
		t.Errorf("%d of 100 lines on disk", mine)
	}

	// 對方放開檔案後，下一次重試就能輪轉（即使本層記錄的大小還沒到上限）
	other.Close()
	rotateRetry = 0
	if _, err := r.Write([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	if r.blocked {
		t.Error("still blocked after the other writer released the file")
	}
	if info, _ := os.Stat(logfile); info == nil || info.Size() > megabyte {
		t.Errorf("app.log was not rotated: %v", info)
	}
}
