// @chris
package main

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version 是 hyp 與生成專案 go.mod 所用的 hypgo 版本（單一來源，不要用 -ldflags 覆寫：
// 先前 Makefile 以 -X main.Version=0.1.0 蓋掉，生成的專案 require 不存在的 hypgo v0.1.0）。
// BuildTime / GitCommit 可由 -ldflags 注入；未注入時取 go build 內嵌的 VCS 資訊
var (
	Version   = "0.9.3"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// buildMeta 回傳 commit 與建置時間：優先用 -ldflags 注入的值，否則取內嵌的 vcs.revision / vcs.time
func buildMeta() (commit, when string) {
	commit, when = GitCommit, BuildTime
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch {
			case s.Key == "vcs.revision" && commit == "unknown":
				commit = s.Value
			case s.Key == "vcs.time" && when == "unknown":
				when = s.Value
			}
		}
	}
	return commit, when
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version information",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		commit, when := buildMeta()
		fmt.Printf("HypGo %s\n", Version)
		fmt.Printf("  Build Time: %s\n", when)
		fmt.Printf("  Git Commit: %s\n", commit)
		fmt.Printf("  Go Version: %s\n", runtime.Version())
		fmt.Printf("  OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
