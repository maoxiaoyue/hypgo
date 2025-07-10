package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	imageName  string
	imageTag   string
	dockerfile string
	noPush     bool
	registry   string
	buildArgs  []string
)

func init() {
	dockerCmd.Flags().StringVarP(&imageName, "name", "n", "", "Docker image name (default: project name)")
	dockerCmd.Flags().StringVarP(&imageTag, "tag", "t", "latest", "Docker image tag")
	dockerCmd.Flags().StringVarP(&dockerfile, "dockerfile", "f", "", "Path to Dockerfile (auto-generated if not specified)")
	dockerCmd.Flags().BoolVar(&noPush, "no-push", true, "Don't push image to registry")
	dockerCmd.Flags().StringVarP(&registry, "registry", "r", "", "Docker registry URL")
	dockerCmd.Flags().StringArrayVar(&buildArgs, "build-arg", []string{}, "Build arguments")
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

	// 2. 讀取配置獲取端口
	port, err := getAppPort()
	if err != nil {
		return err
	}
	fmt.Printf("✅ Detected application port: %s\n", port)

	// 3. 獲取項目名稱
	projectName := getProjectName()
	if imageName == "" {
		imageName = strings.ToLower(projectName)
	}

	// 4. 生成或使用 Dockerfile
	dockerfilePath := dockerfile
	if dockerfilePath == "" {
		dockerfilePath, err = generateDockerfile(port, projectName)
		if err != nil {
			return err
		}
		defer os.Remove(dockerfilePath) // 清理臨時文件
	}

	// 5. 構建 Docker 鏡像
	fullImageName := buildFullImageName()
	if err := buildDockerImage(dockerfilePath, fullImageName); err != nil {
		return err
	}

	// 6. 推送到註冊表（如果需要）
	if !noPush && registry != "" {
		if err := pushDockerImage(fullImageName); err != nil {
			return err
		}
	}

	// 7. 生成運行指令
	printRunInstructions(fullImageName, port)

	return nil
}

func checkPrerequisites() error {
	fmt.Println("🔍 Checking prerequisites...")

	// 檢查 Docker 是否安裝
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("❌ Docker is not installed or not in PATH")
	}

	// 檢查 Docker daemon 是否運行
	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("❌ Docker daemon is not running. Please start Docker first")
	}

	// 檢查是否在項目目錄中
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		return fmt.Errorf("❌ Please run this command in a HypGo project directory")
	}

	// 檢查配置文件
	if _, err := os.Stat("config/config.yaml"); os.IsNotExist(err) {
		return fmt.Errorf("❌ config/config.yaml not found")
	}

	fmt.Println("✅ All prerequisites met")
	return nil
}

func getAppPort() (string, error) {
	viper.SetConfigFile("config/config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	addr := viper.GetString("server.addr")
	if addr == "" {
		return "8080", nil // 默認端口
	}

	// 提取端口號
	if strings.HasPrefix(addr, ":") {
		return addr[1:], nil
	}

	// 處理完整地址格式
	parts := strings.Split(addr, ":")
	if len(parts) >= 2 {
		return parts[len(parts)-1], nil
	}

	return "8080", nil
}

func getProjectName() string {
	// 從 go.mod 獲取模塊名
	data, err := ioutil.ReadFile("go.mod")
	if err != nil {
		return "hypgo-app"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				// 獲取最後一部分作為項目名
				modulePath := parts[1]
				return filepath.Base(modulePath)
			}
		}
	}

	// 使用當前目錄名作為備選
	cwd, _ := os.Getwd()
	return filepath.Base(cwd)
}

func generateDockerfile(port, projectName string) (string, error) {
	fmt.Println("📝 Generating Dockerfile...")

	dockerfileTemplate := `# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o {{.AppName}} .

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 -S hypgo && \
    adduser -u 1000 -S hypgo -G hypgo

WORKDIR /app

# Copy built binary
COPY --from=builder /build/{{.AppName}} .

# Copy configuration files
COPY --from=builder /build/config ./config

# Copy static files if they exist
COPY --from=builder /build/static ./static 2>/dev/null || true
COPY --from=builder /build/templates ./templates 2>/dev/null || true

# Create logs directory
RUN mkdir -p logs && chown -R hypgo:hypgo /app

# Switch to non-root user
USER hypgo

# Expose port
EXPOSE {{.Port}}

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:{{.Port}}/api/health || exit 1

# Run the application
CMD ["./{{.AppName}}"]
`

	tmpl, err := template.New("dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return "", err
	}

	data := struct {
		AppName string
		Port    string
	}{
		AppName: projectName,
		Port:    port,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	// 寫入臨時 Dockerfile
	tmpfile, err := ioutil.TempFile(".", "Dockerfile.tmp.")
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

	args := []string{"build", "-t", fullImageName, "-f", dockerfilePath}

	// 添加構建參數
	for _, arg := range buildArgs {
		args = append(args, "--build-arg", arg)
	}

	args = append(args, ".")

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	startTime := time.Now()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build Docker image: %w", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✅ Docker image built successfully in %s\n", duration.Round(time.Second))

	// 顯示鏡像信息
	showImageInfo(fullImageName)

	return nil
}

func showImageInfo(imageName string) {
	cmd := exec.Command("docker", "images", imageName, "--format", "table {{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.Size}}")
	output, err := cmd.Output()
	if err == nil {
		fmt.Println("\n📊 Image Information:")
		fmt.Println(string(output))
	}
}

func pushDockerImage(fullImageName string) error {
	fmt.Printf("\n📤 Pushing image to registry: %s\n", registry)

	// 檢查是否已登錄
	if err := checkDockerLogin(); err != nil {
		return err
	}

	cmd := exec.Command("docker", "push", fullImageName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push Docker image: %w", err)
	}

	fmt.Println("✅ Image pushed successfully")
	return nil
}

func checkDockerLogin() error {
	// 嘗試執行 docker login 檢查
	cmd := exec.Command("docker", "info")
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	// 簡單檢查是否包含註冊表信息
	if registry != "" && !strings.Contains(string(output), registry) {
		fmt.Printf("⚠️  You may need to login to %s first:\n", registry)
		fmt.Printf("   docker login %s\n", registry)
	}

	return nil
}

func printRunInstructions(fullImageName, port string) {
	fmt.Println("\n🚀 Docker image ready!")
	fmt.Println("========================")
	fmt.Printf("Image: %s\n", fullImageName)
	fmt.Printf("Port: %s\n\n", port)

	fmt.Println("📋 Run commands:")
	fmt.Println("----------------")

	// 基本運行命令
	fmt.Printf("# Run container:\n")
	fmt.Printf("docker run -d -p %s:%s --name %s %s\n\n", port, port, imageName, fullImageName)

	// 帶配置掛載的運行命令
	fmt.Printf("# Run with custom config:\n")
	fmt.Printf("docker run -d -p %s:%s -v $(pwd)/config:/app/config --name %s %s\n\n", port, port, imageName, fullImageName)

	// 帶日誌掛載的運行命令
	fmt.Printf("# Run with logs volume:\n")
	fmt.Printf("docker run -d -p %s:%s -v $(pwd)/logs:/app/logs --name %s %s\n\n", port, port, imageName, fullImageName)

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
      - ./config:/app/config
      - ./logs:/app/logs
    environment:
      - HYPGO_ENV=production
    restart: unless-stopped
    networks:
      - hypgo-network

networks:
  hypgo-network:
    driver: bridge
`, imageName, port, port)

	fmt.Println(composeContent)

	// 詢問是否保存 docker-compose.yml
	fmt.Print("\n💾 Save docker-compose.yml? (y/N): ")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	if response == "y" || response == "yes" {
		if err := ioutil.WriteFile("docker-compose.yml", []byte(composeContent), 0644); err != nil {
			fmt.Printf("❌ Failed to save docker-compose.yml: %v\n", err)
		} else {
			fmt.Println("✅ docker-compose.yml saved successfully")
			fmt.Println("\n# Run with docker-compose:")
			fmt.Println("docker-compose up -d")
		}
	}
}

// 額外的輔助功能

func generateDockerIgnore() error {
	dockerignoreContent := `# Binaries
*.exe
*.dll
*.so
*.dylib
{{.ProjectName}}

# Test binary
*.test

# Output of the go coverage tool
*.out

# Dependency directories
vendor/

# Go workspace file
go.work

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Project specific
logs/
*.log
hypgo.pid
.env
.env.local

# Docker
Dockerfile*
docker-compose*.yml
.dockerignore

# Git
.git/
.gitignore

# Documentation
*.md
docs/
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

	return ioutil.WriteFile(".dockerignore", buf.Bytes(), 0644)
}
