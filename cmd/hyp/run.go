package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var (
	watch bool
)

func init() {
	runCmd.Flags().BoolVarP(&watch, "watch", "w", false, "Enable hot reload")
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the HypGo application",
	RunE:  runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	if watch {
		return runWithWatch()
	}

	return runApp()
}

func runApp() error {
	cmd := exec.Command("go", "run", "main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func runWithWatch() error {
	fmt.Println("🔥 Hot reload enabled")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	// 監視目錄
	dirs := []string{"app", "config"}
	for _, dir := range dirs {
		if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return watcher.Add(path)
			}
			return nil
		}); err != nil {
			fmt.Printf("Warning: %v\n", err)
		}
	}

	var cmd *exec.Cmd
	startApp := func() {
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
			cmd.Wait()
		}

		cmd = exec.Command("go", "run", "main.go")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin

		if err := cmd.Start(); err != nil {
			fmt.Printf("Error starting app: %v\n", err)
		}
	}

	startApp()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create {
				fmt.Printf("🔄 Reloading due to change in: %s\n", event.Name)
				startApp()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Printf("Watch error: %v\n", err)
		}
	}
}
