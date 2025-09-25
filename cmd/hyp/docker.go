package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/spf13/cobra"
)

// ContainerBuilder 純 Go 的容器建構器
type ContainerBuilder struct {
	projectName string
	imageName   string
	imageTag    string
	registry    string
	port        string
	baseImage   string
}

var (
	cb           ContainerBuilder
	outputFormat string
	pushImage    bool
)

func init() {
	containerCmd.Flags().StringVarP(&cb.imageName, "name", "n", "", "Container image name")
	containerCmd.Flags().StringVarP(&cb.imageTag, "tag", "t", "latest", "Container image tag")
	containerCmd.Flags().StringVarP(&cb.registry, "registry", "r", "", "Container registry URL")
	containerCmd.Flags().StringVar(&cb.baseImage, "base", "gcr.io/distroless/static:nonroot", "Base image")
	containerCmd.Flags().StringVar(&outputFormat, "output", "oci", "Output format (oci/docker/tar)")
	containerCmd.Flags().BoolVar(&pushImage, "push", false, "Push to registry")
}

var containerCmd = &cobra.Command{
	Use:   "container",
	Short: "Build container image using pure Go (no Docker required)",
	Long:  `Build OCI/Docker compatible container images without requiring Docker daemon`,
	RunE:  runContainer,
}

func runContainer(cmd *cobra.Command, args []string) error {
	fmt.Println("📦 HypGo Container Builder (Pure Go)")
	fmt.Println("=====================================")

	//準備建構
	if err := cb.prepare(); err != nil {
		return fmt.Errorf("preparation failed: %w", err)
	}

	//編譯應用程式
	fmt.Println("🔨 Building application...")
	if err := cb.buildBinary(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	//建立容器映像
	fmt.Println("🐳 Creating container image...")
	_, err := cb.buildImage()
	if err != nil {
		return fmt.Errorf("image creation failed: %w", err)
	}

	//輸出或推送映像
	if err := cb.outputImage(); err != nil {
		return fmt.Errorf("output failed: %w", err)
	}

	cb.printInstructions()
	return nil
}

// prepare 準備建構環境
func (cb *ContainerBuilder) prepare() error {
	// 獲取專案名稱
	cb.projectName = cb.getProjectName()
	if cb.imageName == "" {
		cb.imageName = strings.ToLower(cb.projectName)
	}

	// 獲取端口
	cb.port = cb.getAppPort()

	fmt.Printf("✅ Project: %s\n", cb.projectName)
	fmt.Printf("✅ Port: %s\n", cb.port)
	fmt.Printf("✅ Base image: %s\n", cb.baseImage)

	return nil
}

// buildBinary 編譯 Go 二進位檔案
func (cb *ContainerBuilder) buildBinary() error {
	// 使用 ko 或直接編譯
	buildCmd := []string{
		"go", "build",
		"-ldflags", "-w -s",
		"-o", cb.projectName,
		".",
	}

	// 設置環境變數
	env := os.Environ()
	env = append(env, "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")

	cmd := exec.Command(buildCmd[0], buildCmd[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("compilation failed: %w", err)
	}

	fmt.Printf("✅ Binary built: %s\n", cb.projectName)
	return nil
}

// buildImage 建立容器映像
func (cb *ContainerBuilder) buildImage() (v1.Image, error) {
	// 方法 1: 使用 ko 風格的建構
	return cb.buildWithKoStyle()

	// 方法 2: 使用 buildah 風格的建構
	// return cb.buildWithBuildahStyle()

	// 方法 3: 使用 go-containerregistry
	// return cb.buildWithContainerRegistry()
}

// buildWithKoStyle 使用 ko 風格建構
func (cb *ContainerBuilder) buildWithKoStyle() (v1.Image, error) {
	// 獲取基礎映像
	base, err := crane.Pull(cb.baseImage)
	if err != nil {
		// 如果無法拉取，使用空映像
		base = empty.Image
	}

	// 建立層
	layer, err := cb.createAppLayer()
	if err != nil {
		return nil, fmt.Errorf("failed to create layer: %w", err)
	}

	// 添加層到基礎映像
	image, err := mutate.AppendLayers(base, layer)
	if err != nil {
		return nil, fmt.Errorf("failed to append layers: %w", err)
	}

	// 設置配置
	cfg, err := image.ConfigFile()
	if err != nil {
		return nil, err
	}

	cfg = cb.updateConfig(cfg)

	image, err = mutate.ConfigFile(image, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to update config: %w", err)
	}

	return image, nil
}

// createAppLayer 建立應用程式層
func (cb *ContainerBuilder) createAppLayer() (v1.Layer, error) {
	// 建立 tar 檔案
	tarPath := fmt.Sprintf("%s.tar", cb.projectName)
	tarFile, err := os.Create(tarPath)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tarPath)

	tw := tar.NewWriter(tarFile)
	defer tw.Close()

	// 添加二進位檔案
	if err := cb.addFileToTar(tw, cb.projectName, "/app/"+cb.projectName); err != nil {
		return nil, err
	}

	// 添加配置檔案
	if err := cb.addDirToTar(tw, "config", "/app/config"); err != nil {
		fmt.Printf("⚠️  No config directory found\n")
	}

	// 添加靜態檔案
	if err := cb.addDirToTar(tw, "static", "/app/static"); err != nil {
		fmt.Printf("⚠️  No static directory found\n")
	}

	tw.Close()
	tarFile.Close()

	// 建立層
	return tarball.LayerFromFile(tarPath)
}

// addFileToTar 添加檔案到 tar
func (cb *ContainerBuilder) addFileToTar(tw *tar.Writer, src, dst string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    dst,
		Mode:    0755,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}

// addDirToTar 添加目錄到 tar
func (cb *ContainerBuilder) addDirToTar(tw *tar.Writer, src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 計算目標路徑
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(dst, relPath)

		// 建立 header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = targetPath

		// 寫入 header
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// 如果是檔案，寫入內容
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(tw, file)
			return err
		}

		return nil
	})
}

// updateConfig 更新映像配置
func (cb *ContainerBuilder) updateConfig(cfg *v1.ConfigFile) *v1.ConfigFile {
	if cfg.Config.Env == nil {
		cfg.Config.Env = []string{}
	}
	if cfg.Config.ExposedPorts == nil {
		cfg.Config.ExposedPorts = map[string]struct{}{}
	}

	// 設置環境變數
	cfg.Config.Env = append(cfg.Config.Env,
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"PORT="+cb.port,
	)

	// 暴露端口
	cfg.Config.ExposedPorts[cb.port+"/tcp"] = struct{}{}

	// 設置入口點
	cfg.Config.Entrypoint = []string{"/app/" + cb.projectName}

	// 設置工作目錄
	cfg.Config.WorkingDir = "/app"

	// 設置標籤
	if cfg.Config.Labels == nil {
		cfg.Config.Labels = map[string]string{}
	}
	cfg.Config.Labels["org.opencontainers.image.source"] = "hypgo"
	cfg.Config.Labels["org.opencontainers.image.created"] = time.Now().Format(time.RFC3339)

	return cfg
}

// outputImage 輸出映像
func (cb *ContainerBuilder) outputImage() error {
	imageName := cb.getFullImageName()
	image, err := cb.buildImage()
	if err != nil {
		return err
	}
	tag, err := name.NewTag(imageName)
	if err != nil {
		return fmt.Errorf("invalid image name: %w", err)
	}
	switch outputFormat {
	case "tar":
		// 輸出為 tar 檔案
		tarPath := fmt.Sprintf("%s.tar", cb.imageName)
		return crane.Save(image, imageName, tarPath)

	case "oci":
		// 輸出為 OCI 格式
		return crane.Push(image, imageName)

	case "docker":
		// 載入到 Docker daemon (如果有)
		_, err := daemon.Write(tag, image)
		return err

	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

// getProjectName 獲取專案名稱
func (cb *ContainerBuilder) getProjectName() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "hypgo_app"
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return filepath.Base(parts[1])
			}
		}
	}

	cwd, _ := os.Getwd()
	return filepath.Base(cwd)
}

// getAppPort 獲取應用程式端口
func (cb *ContainerBuilder) getAppPort() string {
	// 簡化版本，實際應該從配置讀取
	return "8080"
}

// getFullImageName 獲取完整映像名稱
func (cb *ContainerBuilder) getFullImageName() string {
	name := cb.imageName
	if cb.registry != "" {
		name = cb.registry + "/" + name
	}
	return name + ":" + cb.imageTag
}

// printInstructions 列印使用說明
func (cb *ContainerBuilder) printInstructions() {
	fmt.Println("\n🚀 Container image ready!")
	fmt.Println("========================")
	fmt.Printf("Image: %s\n", cb.getFullImageName())
	fmt.Printf("Format: %s\n", outputFormat)

	switch outputFormat {
	case "tar":
		fmt.Printf("\n# Load with Docker:\n")
		fmt.Printf("docker load < %s.tar\n", cb.imageName)
		fmt.Printf("\n# Load with Podman:\n")
		fmt.Printf("podman load < %s.tar\n", cb.imageName)

	case "oci":
		fmt.Printf("\n# Run with any OCI runtime:\n")
		fmt.Printf("ctr run %s\n", cb.getFullImageName())

	case "docker":
		fmt.Printf("\n# Run with Docker:\n")
		fmt.Printf("docker run -p %s:%s %s\n", cb.port, cb.port, cb.getFullImageName())
	}
}

// Alternative: 使用 ko 套件
func buildWithKo() error {
	// Ko 是 Google 的純 Go 容器建構工具
	// 可以直接整合到程式碼中

	/*
		import "github.com/google/ko/pkg/build"
		import "github.com/google/ko/pkg/publish"

		builder, err := build.NewGo(ctx, ".")
		if err != nil {
			return err
		}

		result, err := builder.Build(ctx, ".")
		if err != nil {
			return err
		}

		publisher, err := publish.NewDefault(repo)
		if err != nil {
			return err
		}

		ref, err := publisher.Publish(ctx, result)
	*/

	return nil
}
