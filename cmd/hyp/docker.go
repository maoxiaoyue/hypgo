package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	imageName   string
	imageTag    string
	dockerfile  string
	noPush      bool
	registry    string
	buildArgs   []string
	useRootless bool
	goVersion   string
	platform    string
)

func init() {
	dockerCmd.Flags().StringVarP(&imageName, "name", "n", "", "Docker image name (default: project name)")
	dockerCmd.Flags().StringVarP(&imageTag, "tag", "t", "latest", "Docker image tag")
	dockerCmd.Flags().StringVarP(&dockerfile, "dockerfile", "f", "", "Path to Dockerfile (auto-generated if not specified)")
	dockerCmd.Flags().BoolVar(&noPush, "no-push", true, "Don't push image to registry")
	dockerCmd.Flags().StringVarP(&registry, "registry", "r", "", "Docker registry URL")
	dockerCmd.Flags().StringArrayVar(&buildArgs, "build-arg", []string{}, "Build arguments")
	dockerCmd.Flags().BoolVar(&useRootless, "rootless", true, "Use rootless container (recommended)")
	dockerCmd.Flags().StringVar(&goVersion, "go-version", "", "Go version to use (default: auto-detect)")
	dockerCmd.Flags().StringVar(&platform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
}

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Build and package the application as a Docker container",
	Long:  `Build a Docker image for your HypGo application with automatic port detection and configuration`,
	RunE:  runDocker,
}

func runDocker(cmd *cobra.Command, args []string) error {
	fmt.Println("🐳 HypGo Docker Builder")
	fmt.Println("=======================")

	// 1. 檢查前置條件
	if err := checkPrerequisites(); err != nil {
		return err
	}

	// 2. 檢測 Go 版本
	if goVersion == "" {
		goVersion = detectGoVersion()
	}
	fmt.Printf("✅ Using Go version: %s\n", goVersion)

	// 3. 讀取配置獲取端口
	port, err := getAppPort()
	if err != nil {
		return err
	}
	fmt.Printf("✅ Detected application port: %s\n", port)

	// 4. 獲取項目名稱
	projectName := getProjectName()
	if imageName == "" {
		imageName = strings.ToLower(projectName)
	}

	// 5. 生成 .dockerignore
	if err := generateDockerIgnore(); err != nil {
		fmt.Printf("⚠️  Warning: Failed to generate .dockerignore: %v\n", err)
	}

	// 6. 生成或使用 Dockerfile
	dockerfilePath := dockerfile
	if dockerfilePath == "" {
		dockerfilePath, err = generateDockerfile(port, projectName)
		if err != nil {
			return err
		}
		defer os.Remove(dockerfilePath) // 清理臨時文件
	}

	// 7. 構建 Docker 鏡像
	fullImageName := buildFullImageName()
	if err := buildDockerImage(dockerfilePath, fullImageName); err != nil {
		return err
	}

	// 8. 推送到註冊表（如果需要）
	if !noPush && registry != "" {
		if err := pushDockerImage(fullImageName); err != nil {
			return err
		}
	}

	// 9. 生成運行指令
	printRunInstructions(fullImageName, port)

	return nil
}

func checkPrerequisites() error {
	fmt.Println("🔍 Checking prerequisites...")

	// 檢查 Docker 或 Podman
	dockerCmd := detectContainerRuntime()
	if dockerCmd == "" {
		return fmt.Errorf(`❌ Container runtime not found. Please install one of the following:
   - Docker Desktop: https://www.docker.com/products/docker-desktop
   - Podman: https://podman.io/getting-started/installation
   - Docker Engine: https://docs.docker.com/engine/install/`)
	}
	fmt.Printf("✅ Found container runtime: %s\n", dockerCmd)

	// 檢查 daemon 是否運行
	cmd := exec.Command(dockerCmd, "info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 檢查是否是權限問題
		if strings.Contains(string(output), "permission denied") {
			return fmt.Errorf(`❌ Permission denied. Try one of these solutions:
   1. Add your user to the docker group:
      sudo usermod -aG docker $USER
      (then logout and login again)
   
   2. Use rootless mode (recommended):
      %s run --rootless ...
   
   3. Use sudo (not recommended):
      sudo %s ...`, dockerCmd, dockerCmd)
		}
		return fmt.Errorf("❌ Container daemon is not running. Please start %s first", dockerCmd)
	}

	// 檢查是否在項目目錄中
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		return fmt.Errorf("❌ Please run this command in a HypGo project directory")
	}

	fmt.Println("✅ All prerequisites met")
	return nil
}

func detectContainerRuntime() string {
	// 優先順序：docker > podman > nerdctl
	runtimes := []string{"docker", "podman", "nerdctl"}
	for _, rt := range runtimes {
		if _, err := exec.LookPath(rt); err == nil {
			return rt
		}
	}
	return ""
}

func detectGoVersion() string {
	// 從 go.mod 讀取版本
	data, err := os.ReadFile("go.mod")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "go ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					return parts[1]
				}
			}
		}
	}

	// 從系統獲取版本
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err == nil {
		// 解析 "go version go1.21.0 linux/amd64"
		parts := strings.Fields(string(output))
		if len(parts) >= 3 {
			version := strings.TrimPrefix(parts[2], "go")
			return version
		}
	}

	// 默認使用最新穩定版
	return "1.21"
}

func getAppPort() (string, error) {
	// 嘗試從多個配置文件讀取
	configFiles := []string{
		"config/config.yaml",
		"config/config.yml",
		"config.yaml",
		"config.yml",
		".env",
	}

	for _, file := range configFiles {
		if _, err := os.Stat(file); err == nil {
			viper.SetConfigFile(file)
			if err := viper.ReadInConfig(); err == nil {
				addr := viper.GetString("server.addr")
				if addr == "" {
					addr = viper.GetString("SERVER_ADDR")
				}
				if addr == "" {
					addr = viper.GetString("PORT")
				}

				if addr != "" {
					// 提取端口號
					if strings.HasPrefix(addr, ":") {
						return addr[1:], nil
					}
					parts := strings.Split(addr, ":")
					if len(parts) >= 2 {
						return parts[len(parts)-1], nil
					}
					return addr, nil
				}
			}
		}
	}

	return "8080", nil // 默認端口
}

func getProjectName() string {
	// 從 go.mod 獲取模塊名
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "hypgo-app"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				modulePath := parts[1]
				return filepath.Base(modulePath)
			}
		}
	}

	cwd, _ := os.Getwd()
	return filepath.Base(cwd)
}

func generateDockerfile(port, projectName string) (string, error) {
	fmt.Println("📝 Generating optimized Dockerfile...")

	// 判斷是否使用 rootless
	userSection := ""
	if useRootless {
		userSection = `
# Create non-root user
RUN addgroup -g 1001 -S hypgo && \
    adduser -u 1001 -S hypgo -G hypgo

# Set ownership
RUN chown -R hypgo:hypgo /app

# Switch to non-root user
USER hypgo`
	}

	// 多階段構建的 Dockerfile 模板
	dockerfileTemplate := `# Build stage
FROM golang:{{.GoVersion}}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Verify dependencies
RUN go mod verify

# Copy source code
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH={{.Arch}} \
    go build -ldflags="-w -s" \
    -a -installsuffix cgo \
    -o {{.AppName}} .

# Runtime stage - use distroless for security
FROM gcr.io/distroless/static-debian12:nonroot

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Set timezone
ENV TZ=UTC

WORKDIR /app

# Copy built binary with specific permissions
COPY --from=builder --chown=nonroot:nonroot /build/{{.AppName}} ./

# Copy configuration files
COPY --from=builder --chown=nonroot:nonroot /build/config ./config

# Copy static assets if they exist
COPY --from=builder --chown=nonroot:nonroot /build/static ./static 2>/dev/null || true
COPY --from=builder --chown=nonroot:nonroot /build/templates ./templates 2>/dev/null || true
{{.UserSection}}

# Expose port
EXPOSE {{.Port}}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/{{.AppName}}", "health"] || exit 1

# Run the application
ENTRYPOINT ["/app/{{.AppName}}"]
`

	// 如果不使用 rootless，使用傳統 alpine
	if !useRootless {
		dockerfileTemplate = `# Build stage
FROM golang:{{.GoVersion}}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH={{.Arch}} \
    go build -ldflags="-w -s" -a -installsuffix cgo -o {{.AppName}} .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy built binary
COPY --from=builder /build/{{.AppName}} .

# Copy configuration files
COPY --from=builder /build/config ./config

# Copy static files if they exist
COPY --from=builder /build/static ./static 2>/dev/null || true
COPY --from=builder /build/templates ./templates 2>/dev/null || true
{{.UserSection}}

# Expose port
EXPOSE {{.Port}}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:{{.Port}}/health || exit 1

# Run the application
CMD ["./{{.AppName}}"]
`
	}

	// 檢測架構
	arch := runtime.GOARCH
	if arch == "arm64" {
		arch = "arm64"
	} else {
		arch = "amd64"
	}

	tmpl, err := template.New("dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return "", err
	}

	data := struct {
		AppName     string
		Port        string
		GoVersion   string
		Arch        string
		UserSection string
	}{
		AppName:     projectName,
		Port:        port,
		GoVersion:   goVersion,
		Arch:        arch,
		UserSection: userSection,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	// 寫入臨時 Dockerfile
	tmpfile, err := os.CreateTemp(".", "Dockerfile.tmp.")
	if err != nil {
		return "", err
	}

	if _, err := tmpfile.Write(buf.Bytes()); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		return "", err
	}

	tmpfile.Close()
	return tmpfile.Name(), nil
}

func buildFullImageName() string {
	name := imageName
	if registry != "" {
		name = fmt.Sprintf("%s/%s", strings.TrimSuffix(registry, "/"), imageName)
	}
	return fmt.Sprintf("%s:%s", name, imageTag)
}

func buildDockerImage(dockerfilePath, fullImageName string) error {
	fmt.Printf("\n🔨 Building Docker image: %s\n", fullImageName)

	containerCmd := detectContainerRuntime()
	args := []string{"build", "-t", fullImageName, "-f", dockerfilePath}

	// 添加平台參數
	if platform != "" {
		args = append(args, "--platform", platform)
	}

	// 添加構建參數
	for _, arg := range buildArgs {
		args = append(args, "--build-arg", arg)
	}

	// 添加構建進度顯示
	args = append(args, "--progress=plain")

	args = append(args, ".")

	cmd := exec.Command(containerCmd, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	startTime := time.Now()
	if err := cmd.Run(); err != nil {
		// 提供更詳細的錯誤信息
		return fmt.Errorf(`failed to build Docker image: %w

Troubleshooting tips:
1. Check if Docker daemon is running
2. Ensure you have enough disk space
3. Try building with --no-cache flag
4. Check Docker logs: docker logs`, err)
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✅ Docker image built successfully in %s\n", duration.Round(time.Second))

	// 顯示鏡像信息
	showImageInfo(fullImageName)

	return nil
}

func showImageInfo(imageName string) {
	containerCmd := detectContainerRuntime()
	cmd := exec.Command(containerCmd, "images", imageName, "--format", "table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}")
	output, err := cmd.Output()
	if err == nil {
		fmt.Println("\n📊 Image Information:")
		fmt.Println(string(output))
	}
}

func pushDockerImage(fullImageName string) error {
	fmt.Printf("\n📤 Pushing image to registry: %s\n", registry)

	containerCmd := detectContainerRuntime()

	// 檢查是否已登錄
	if err := checkDockerLogin(); err != nil {
		return err
	}

	cmd := exec.Command(containerCmd, "push", fullImageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push Docker image: %w", err)
	}

	fmt.Println("✅ Image pushed successfully")
	return nil
}

func checkDockerLogin() error {
	containerCmd := detectContainerRuntime()
	cmd := exec.Command(containerCmd, "info")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if registry != "" && !strings.Contains(string(output), registry) {
		fmt.Printf("⚠️  You may need to login to %s first:\n", registry)
		fmt.Printf("   %s login %s\n", containerCmd, registry)
	}

	return nil
}

func printRunInstructions(fullImageName, port string) {
	containerCmd := detectContainerRuntime()

	fmt.Println("\n🚀 Docker image ready!")
	fmt.Println("========================")
	fmt.Printf("Image: %s\n", fullImageName)
	fmt.Printf("Port: %s\n\n", port)

	fmt.Println("📋 Run commands:")
	fmt.Println("----------------")

	// 基本運行命令
	fmt.Printf("# Run container:\n")
	fmt.Printf("%s run -d -p %s:%s --name %s %s\n\n", containerCmd, port, port, imageName, fullImageName)

	// Rootless 模式
	if useRootless {
		fmt.Printf("# Run in rootless mode (more secure):\n")
		fmt.Printf("%s run -d --userns=host -p %s:%s --name %s %s\n\n", containerCmd, port, port, imageName, fullImageName)
	}

	// 帶配置掛載的運行命令
	fmt.Printf("# Run with custom config:\n")
	fmt.Printf("%s run -d -p %s:%s -v $(pwd)/config:/app/config:ro --name %s %s\n\n", containerCmd, port, port, imageName, fullImageName)

	// 帶日誌掛載的運行命令
	fmt.Printf("# Run with logs volume:\n")
	fmt.Printf("%s run -d -p %s:%s -v hypgo-logs:/app/logs --name %s %s\n\n", containerCmd, port, port, imageName, fullImageName)

	// Docker Compose 示例
	fmt.Println("# Docker Compose example:")
	fmt.Println("------------------------")
	generateDockerCompose(fullImageName, port)
}

func generateDockerCompose(imageName, port string) {
	composeContent := fmt.Sprintf(`version: '3.8'

services:
  app:
    image: %s
    ports:
      - "%s:%s"
    volumes:
      - ./config:/app/config:ro
      - hypgo-logs:/app/logs
    environment:
      - HYPGO_ENV=production
      - TZ=UTC
    restart: unless-stopped
    networks:
      - hypgo-network
    deploy:
      resources:
        limits:
          memory: 512M
        reservations:
          memory: 256M
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:%s/health"]
      interval: 30s
      timeout: 3s
      retries: 3

volumes:
  hypgo-logs:
    driver: local

networks:
  hypgo-network:
    driver: bridge
`, imageName, port, port, port)

	fmt.Println(composeContent)

	// 詢問是否保存 docker-compose.yml
	fmt.Print("\n💾 Save docker-compose.yml? (y/N): ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "y" || response == "yes" {
		if err := os.WriteFile("docker-compose.yml", []byte(composeContent), 0644); err != nil {
			fmt.Printf("❌ Failed to save docker-compose.yml: %v\n", err)
		} else {
			fmt.Println("✅ docker-compose.yml saved successfully")
			fmt.Println("\n# Run with docker-compose:")
			fmt.Println("docker-compose up -d")
		}
	}
}

func generateDockerIgnore() error {
	dockerignoreContent := `# Binaries
*.exe
*.dll
*.so
*.dylib
*_test.go

# Build artifacts
/{{.ProjectName}}
/bin/
/dist/
/build/

# Test binary
*.test

# Coverage
*.out
*.cov
coverage.txt
coverage.html

# Dependency directories
vendor/

# Go workspace
go.work
go.work.sum

# IDE
.idea/
.vscode/
*.swp
*.swo
*~
.DS_Store

# OS
Thumbs.db
.DS_Store

# Project specific
logs/
*.log
*.pid
.env
.env.*
!.env.example

# Docker
Dockerfile*
docker-compose*.yml
.dockerignore

# Git
.git/
.gitignore
.github/

# Documentation
*.md
docs/
LICENSE

# Temporary files
tmp/
temp/
*.tmp
*.bak
*.backup
`

	projectName := getProjectName()
	tmpl, err := template.New("dockerignore").Parse(dockerignoreContent)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ ProjectName string }{projectName}); err != nil {
		return err
	}

	return os.WriteFile(".dockerignore", buf.Bytes(), 0644)
}
