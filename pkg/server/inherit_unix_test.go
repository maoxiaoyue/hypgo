//go:build !windows

package server

import (
	"net"
	"os"
	"testing"
)

// TestFdIsListeningSocket 驗證 SO_ACCEPTCONN 檢查能分辨三種 FD：
// listening socket（應繼承）、已連線 socket 與一般檔案（bugs.md #1 的誤繼承情境，應擋下）。
func TestFdIsListeningSocket(t *testing.T) {
	// listening socket → true
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	lf, err := ln.(*net.TCPListener).File()
	if err != nil {
		t.Fatal(err)
	}
	defer lf.Close()
	if !fdIsListeningSocket(int(lf.Fd())) {
		t.Error("listening socket should be detected as listening")
	}

	// 已連線 socket → false（誤繼承後 Accept 會爆 accept4: invalid argument）
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cf, err := conn.(*net.TCPConn).File()
	if err != nil {
		t.Fatal(err)
	}
	defer cf.Close()
	if fdIsListeningSocket(int(cf.Fd())) {
		t.Error("connected socket must not be treated as an inherited listener")
	}

	// 一般檔案 → false（getsockopt 回 ENOTSOCK）
	f, err := os.CreateTemp(t.TempDir(), "notasocket")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if fdIsListeningSocket(int(f.Fd())) {
		t.Error("regular file must not be treated as an inherited listener")
	}
}
