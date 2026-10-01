package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// cToolchain 描述 Desktop（Fyne）專案編譯所需的 C 工具鏈狀態。
// cgo 本身內建於 Go，真正可能缺的是 C 編譯器，以及 CGO_ENABLED 被關閉。
type cToolchain struct {
	Compiler   string // 找到的 C 編譯器路徑；空字串表示沒找到
	CGOEnabled bool   // go env CGO_ENABLED 是否為 1
}

// Ready 回報工具鏈是否足以編譯 Fyne。
func (t cToolchain) Ready() bool {
	return t.Compiler != "" && t.CGOEnabled
}

// detectCToolchain 偵測 C 編譯器與 CGO_ENABLED。
// 編譯器依序找 $CC、gcc、clang、cc；找不到 go 時 CGOEnabled 視為 false。
func detectCToolchain() cToolchain {
	var t cToolchain

	candidates := []string{"gcc", "clang", "cc"}
	if cc := strings.TrimSpace(os.Getenv("CC")); cc != "" {
		// CC 可能帶參數（如 "zig cc"），只取執行檔部分
		candidates = append([]string{strings.Fields(cc)[0]}, candidates...)
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			t.Compiler = p
			break
		}
	}

	if out, err := exec.Command("go", "env", "CGO_ENABLED").Output(); err == nil {
		t.CGOEnabled = strings.TrimSpace(string(out)) == "1"
	}
	return t
}

// desktopInstallCommand 回傳在指定平台安裝 Fyne 編譯依賴的命令。
// has 用來查詢某個執行檔是否存在（方便測試注入）。
// 找不到可用的套件管理器時回傳 nil。
func desktopInstallCommand(goos string, has func(string) bool) []string {
	switch goos {
	case "windows":
		if has("winget") {
			return []string{"winget", "install", "-e", "--id", "BrechtSanders.WinLibs.POSIX.UCRT"}
		}
	case "darwin":
		if has("xcode-select") {
			return []string{"xcode-select", "--install"}
		}
	case "linux":
		// Linux 除了 gcc 還需要 X11 / OpenGL 開發標頭檔
		switch {
		case has("apt-get"):
			return []string{"sudo", "apt-get", "install", "-y", "gcc", "libgl1-mesa-dev", "xorg-dev"}
		case has("dnf"):
			return []string{"sudo", "dnf", "install", "-y", "gcc", "libXcursor-devel", "libXrandr-devel",
				"mesa-libGL-devel", "libXi-devel", "libXinerama-devel", "libXxf86vm-devel"}
		case has("pacman"):
			return []string{"sudo", "pacman", "-S", "--needed", "--noconfirm", "gcc", "xorg-server-devel",
				"libxcursor", "libxrandr", "libxinerama", "libxi"}
		}
	}
	return nil
}

// desktopManualHint 回傳沒有可用套件管理器時的手動安裝說明。
func desktopManualHint(goos string) string {
	switch goos {
	case "windows":
		return "Install MSYS2 (https://www.msys2.org/) or TDM-GCC (https://jmeubank.github.io/tdm-gcc/) and add gcc to PATH"
	case "darwin":
		return "Install Xcode Command Line Tools"
	case "linux":
		return "Install gcc plus the X11 / OpenGL development headers with your distro's package manager"
	default:
		return "Install a C compiler (gcc or clang) and add it to PATH"
	}
}

// reportDesktopDeps 依偵測結果輸出提示：就緒時只印一行確認，
// 缺少時印出原因與對應平台的安裝命令。回傳是否仍有缺漏。
func reportDesktopDeps(w io.Writer, t cToolchain, goos string, install []string) bool {
	if t.Ready() {
		fmt.Fprintf(w, "\n✓ C toolchain ready (%s, CGO_ENABLED=1)\n", t.Compiler)
		if goos == "linux" {
			fmt.Fprintf(w, "  Fyne also needs X11 / OpenGL dev headers; if the build fails, re-run with --install-deps\n")
		}
		return false
	}

	fmt.Fprintf(w, "\n⚠️  Fyne needs cgo, but the C toolchain is not ready:\n")
	if t.Compiler == "" {
		fmt.Fprintf(w, "   - no C compiler (gcc / clang) found in PATH\n")
		if len(install) > 0 {
			fmt.Fprintf(w, "     install: %s\n", strings.Join(install, " "))
			fmt.Fprintf(w, "     or let hyp run it for you: hyp new desktop <name> --install-deps\n")
		} else {
			fmt.Fprintf(w, "     %s\n", desktopManualHint(goos))
		}
	}
	if !t.CGOEnabled {
		if t.Compiler == "" && goos == "windows" {
			fmt.Fprintf(w, "   - CGO_ENABLED=0 (Go turns it off when no C compiler is found; it comes back once gcc is in PATH)\n")
		} else {
			fmt.Fprintf(w, "   - CGO_ENABLED=0\n")
			fmt.Fprintf(w, "     enable: go env -w CGO_ENABLED=1\n")
		}
	}
	fmt.Fprintf(w, "   The project was created; `go run .` will work once the above is fixed.\n")
	return true
}

// installDesktopDeps 執行安裝命令。執行前先印出完整命令；
// 只在使用者明確加上 --install-deps 時才會被呼叫。
func installDesktopDeps(install []string) error {
	fmt.Printf("\n🔧 --install-deps: running\n   %s\n", strings.Join(install, " "))

	cmd := exec.Command(install[0], install[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ensureDesktopDeps 是 hyp new desktop 的依賴檢查入口：
// 偵測 → 提示 →（installDeps 為 true 時）代為安裝。
// 任何失敗都只印警告，不讓專案建立流程中斷。
func ensureDesktopDeps(installDeps bool) {
	goos := runtime.GOOS
	has := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}

	t := detectCToolchain()
	install := desktopInstallCommand(goos, has)

	// Linux 上 gcc 存在不代表標頭檔齊全，所以明確要求時一律執行（套件管理器會略過已安裝者）
	wantInstall := installDeps && (t.Compiler == "" || goos == "linux")
	if !wantInstall {
		reportDesktopDeps(os.Stdout, t, goos, install)
		return
	}

	if len(install) == 0 {
		fmt.Fprintf(os.Stderr, "\nWarning: --install-deps: no supported package manager found.\n  %s\n", desktopManualHint(goos))
		return
	}
	if err := installDesktopDeps(install); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: dependency install failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Run it manually: %s\n", strings.Join(install, " "))
		return
	}

	// 安裝程式改的 PATH 通常不會反映到目前這個行程，重新偵測後如實回報
	if after := detectCToolchain(); after.Ready() {
		reportDesktopDeps(os.Stdout, after, goos, install)
	} else {
		fmt.Printf("\n✓ Install command finished. Open a new terminal so the updated PATH takes effect, then run `go run .`\n")
	}
}
