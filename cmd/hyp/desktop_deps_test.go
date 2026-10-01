package main

import (
	"bytes"
	"strings"
	"testing"
)

func hasOnly(names ...string) func(string) bool {
	return func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}
}

func TestDesktopInstallCommand(t *testing.T) {
	cases := []struct {
		goos  string
		has   []string
		first string // 預期命令的前綴；空字串表示應回傳 nil
	}{
		{"windows", []string{"winget"}, "winget install"},
		{"windows", nil, ""},
		{"darwin", []string{"xcode-select"}, "xcode-select --install"},
		{"linux", []string{"apt-get"}, "sudo apt-get install -y gcc"},
		{"linux", []string{"dnf"}, "sudo dnf install -y gcc"},
		{"linux", []string{"pacman"}, "sudo pacman -S"},
		{"linux", []string{"apt-get", "dnf"}, "sudo apt-get"},
		{"linux", nil, ""},
		{"freebsd", []string{"pkg"}, ""},
	}
	for _, c := range cases {
		got := strings.Join(desktopInstallCommand(c.goos, hasOnly(c.has...)), " ")
		if c.first == "" {
			if got != "" {
				t.Errorf("%s %v: expected no command, got %q", c.goos, c.has, got)
			}
			continue
		}
		if !strings.HasPrefix(got, c.first) {
			t.Errorf("%s %v: got %q, want prefix %q", c.goos, c.has, got, c.first)
		}
	}
}

func TestReportDesktopDeps(t *testing.T) {
	install := []string{"winget", "install", "x"}

	t.Run("ready", func(t *testing.T) {
		var buf bytes.Buffer
		missing := reportDesktopDeps(&buf, cToolchain{Compiler: "/usr/bin/gcc", CGOEnabled: true}, "windows", install)
		if missing {
			t.Error("ready toolchain should not report missing")
		}
		if strings.Contains(buf.String(), "⚠") || strings.Contains(buf.String(), "install:") {
			t.Errorf("ready toolchain should not warn: %s", buf.String())
		}
	})

	t.Run("no compiler with package manager", func(t *testing.T) {
		var buf bytes.Buffer
		missing := reportDesktopDeps(&buf, cToolchain{}, "windows", install)
		out := buf.String()
		if !missing {
			t.Error("should report missing")
		}
		for _, want := range []string{"no C compiler", "winget install x", "--install-deps"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
	})

	t.Run("no compiler without package manager", func(t *testing.T) {
		var buf bytes.Buffer
		reportDesktopDeps(&buf, cToolchain{}, "windows", nil)
		if !strings.Contains(buf.String(), "MSYS2") {
			t.Errorf("should fall back to manual hint:\n%s", buf.String())
		}
	})

	t.Run("compiler present but cgo disabled", func(t *testing.T) {
		var buf bytes.Buffer
		missing := reportDesktopDeps(&buf, cToolchain{Compiler: "/usr/bin/gcc"}, "linux", install)
		out := buf.String()
		if !missing {
			t.Error("should report missing")
		}
		if !strings.Contains(out, "go env -w CGO_ENABLED=1") {
			t.Errorf("should tell how to enable cgo:\n%s", out)
		}
		if strings.Contains(out, "no C compiler") {
			t.Errorf("should not claim the compiler is missing:\n%s", out)
		}
	})
}
