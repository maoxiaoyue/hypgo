// @chris
package main

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

var newCmd = &cobra.Command{
	Use:   "new [type] [project-name]",
	Short: "Create a new HypGo project",
	Long: `Create a new HypGo project. Specify the project type:

  hyp new <name>              Full-stack web project (default)
  hyp new web <name>          Full-stack web project (explicit)
  hyp new api <name>          API-only project (same as hyp api <name>)
  hyp new cli <name>          CLI tool project (Cobra + services)
  hyp new desktop <name>      Desktop application (Fyne GUI + services)
  hyp new grpc <name>         gRPC microservice (Protobuf + services)

Without a name the project is generated into the current directory, which must
already have a go.mod (go mod init). The module path comes from that go.mod,
existing files are kept, and the requirements the project needs are added to it:

  mkdir billing && cd billing && go mod init github.com/acme/billing
  hyp new api                 API project in the current directory
  hyp new cli                 CLI project in the current directory
  hyp new                     Web project in the current directory

If the directory already holds more than go.mod, go.sum, VCS / editor files, a
README or a LICENSE, pass --force (existing files are still never overwritten).

Examples:
  hyp new myapp               Full-stack web project
  hyp new cli my-tool         CLI tool project
  hyp new desktop mydesktop   Desktop application
  hyp new grpc user-service   gRPC microservice`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			return fmt.Errorf("unknown project type: %s (use web, api, cli, desktop, or grpc)", args[0])
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := resolveProjectTarget(cmd, args, false)
		if err != nil {
			return err
		}
		return runNew(t)
	},
}

var newWebCmd = &cobra.Command{
	Use:   "web [project-name]",
	Short: "Create a new full-stack web project",
	Long: `Create a new full-stack web project (the same as hyp new <name>).
Without a name, generate into the current directory (needs a go.mod).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := resolveProjectTarget(cmd, args, false)
		if err != nil {
			return err
		}
		return runNew(t)
	},
}

var newAPICmd = &cobra.Command{
	Use:   "api [project-name]",
	Short: "Create a new API-only project (same as hyp api)",
	Long: `Create a new API-only project (the same as hyp api <name>).
Without a name, generate into the current directory (needs a go.mod).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAPI,
}

var newCLICmd = &cobra.Command{
	Use:   "cli [project-name]",
	Short: "Create a new CLI tool project",
	Long: `Create a new CLI tool project with Cobra command structure.
Without a name, generate into the current directory (needs a go.mod).

Generated structure:
  mytool/
  ├── app/
  │   ├── commands/      CLI subcommands (Cobra)
  │   │   └── root.go    Root command
  │   ├── models/        Data structures
  │   └── services/      Business logic + Error Catalog
  ├── config/config.yaml Runtime config (loaded in main.go → commands.Cfg)
  ├── main.go            Entry point
  └── go.mod

After creation:
  cd mytool && go mod tidy && go run .

Add subcommands:
  hyp generate command process
  hyp generate command export`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := resolveProjectTarget(cmd, args, true)
		if err != nil {
			return err
		}
		return runNewCLI(t)
	},
}

var newDesktopCmd = &cobra.Command{
	Use:   "desktop [project-name]",
	Short: "Create a new Desktop application (Fyne GUI)",
	Long: `Create a new Desktop application using Fyne GUI toolkit.
Without a name, generate into the current directory (needs a go.mod).

Generated structure:
  myapp/
  ├── app/
  │   ├── views/         GUI views (Fyne widgets + layouts)
  │   │   └── main_view.go
  │   ├── models/        Data structures
  │   └── services/      Business logic + Error Catalog
  ├── config/config.yaml Runtime config (loaded in main.go)
  ├── main.go            Entry point (config → logger → Fyne app + window)
  └── go.mod

After creation:
  cd myapp && go mod tidy && go run .

Add views:
  hyp generate view settings
  hyp generate view dashboard

Requires: C compiler (gcc) for CGO (Fyne dependency).
The toolchain is checked after creation; pass --install-deps to let hyp
run the platform's install command (winget / xcode-select / apt-get / dnf / pacman).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		installDeps, _ := cmd.Flags().GetBool("install-deps")
		t, err := resolveProjectTarget(cmd, args, true)
		if err != nil {
			return err
		}
		return runNewDesktop(t, installDeps)
	},
}

var newGRPCCmd = &cobra.Command{
	Use:   "grpc [project-name]",
	Short: "Create a new gRPC microservice project",
	Long: `Create a new gRPC microservice project with Protobuf definitions.
Without a name, generate into the current directory (needs a go.mod).

Generated structure:
  myservice/
  ├── app/
  │   ├── proto/<name>pb/    Protobuf definitions (.proto)
  │   ├── rpc/               gRPC server implementations
  │   ├── models/            Data structures
  │   └── services/          Business logic + Error Catalog
  ├── config/config.yaml     Runtime config: server.addr / server.tls (loaded in main.go)
  ├── main.go                gRPC server entry point
  ├── Makefile               protoc compilation commands
  └── go.mod

After creation:
  cd myservice && go mod tidy
  make proto                 Compile .proto → Go code
  go run .

Add more services:
  hyp generate proto order

Requires: protoc + protoc-gen-go + protoc-gen-go-grpc`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := resolveProjectTarget(cmd, args, true)
		if err != nil {
			return err
		}
		return runNewGRPC(t)
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newWebCmd)
	newCmd.AddCommand(newAPICmd)
	newCmd.AddCommand(newCLICmd)
	newCmd.AddCommand(newDesktopCmd)
	newCmd.AddCommand(newGRPCCmd)

	newDesktopCmd.Flags().Bool("install-deps", false, "Install the C toolchain Fyne needs via the platform package manager")
	newCmd.PersistentFlags().Bool("force", false, "Generate into a directory that already has project files (existing files are never overwritten)")
}

// projectTarget 一次 hyp new / hyp api 的生成目標
type projectTarget struct {
	dir     string // 目標目錄；就地生成時為 "."
	name    string // 專案名稱：顯示用，CLI / Desktop / gRPC 另用於識別字
	module  string // go.mod 的 module 路徑（生成程式碼的 import 前綴）
	inPlace bool   // 不給名稱、生成到目前目錄
}

// label 是訊息裡顯示的專案目錄
func (t projectTarget) label() string {
	if t.inPlace {
		return "."
	}
	return t.dir
}

// printCD 印出「cd <dir>」步驟；就地生成時不需要
func (t projectTarget) printCD() {
	if !t.inPlace {
		fmt.Printf("   cd %s\n", t.dir)
	}
}

// resolveProjectTarget 由參數決定生成目標。strictName 為 true 時（CLI / Desktop / gRPC）
// 名稱必須能轉成 Go 識別字。
//
// 不給名稱 → 就地生成：目前目錄必須已有 go.mod（go mod init 過），module 路徑取自它，
// 既有檔案一律保留。給名稱 → 建立同名目錄；--force 進入已有 go.mod 的目錄時同樣沿用它的
// module 路徑（先前一律拿目錄名當 module，與既有 go.mod 不符時生成的 import 全部錯誤）
func resolveProjectTarget(cmd *cobra.Command, args []string, strictName bool) (projectTarget, error) {
	if len(args) == 0 {
		return inPlaceTarget(cmd, strictName)
	}
	name := args[0]
	if err := checkNewProject(cmd, name); err != nil {
		return projectTarget{}, err
	}
	t := projectTarget{dir: name, name: name, module: name}
	if data, err := os.ReadFile(filepath.Join(name, "go.mod")); err == nil {
		if mod := modfile.ModulePath(data); mod != "" {
			t.module = mod
		}
	}
	return t, nil
}

// inPlaceFresh 是「剛 go mod init 完」的目錄裡可以有的項目；有其他東西時就地生成要加 --force，
// 避免在既有專案裡誤下 hyp new 而倒進一整套骨架
var inPlaceFresh = regexp.MustCompile(`(?i)^(go\.mod|go\.sum|go\.work|go\.work\.sum|\.git|\.gitignore|\.gitattributes|\.github|\.idea|\.vscode|\.DS_Store|README(\..*)?|LICENSE(\..*)?)$`)

// inPlaceTarget 處理不給名稱的情況：以目前目錄的 go.mod 為準
func inPlaceTarget(cmd *cobra.Command, strictName bool) (projectTarget, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return projectTarget{}, fmt.Errorf("no project name given and no go.mod in the current directory: " +
			"pass a name (hyp new <name>), or run `go mod init <module>` here first")
	}
	module := modfile.ModulePath(data)
	if module == "" {
		return projectTarget{}, fmt.Errorf("go.mod has no module directive")
	}

	name := moduleBaseName(module)
	if strictName && scaffold.ValidateProjectName(name) != nil {
		// module 最後一段不能當識別字時退而用目錄名
		wd, _ := os.Getwd()
		if alt := filepath.Base(wd); scaffold.ValidateProjectName(alt) == nil {
			name = alt
		} else {
			return projectTarget{}, fmt.Errorf("cannot derive a project name from module %q or directory %q "+
				"(letters, digits, underscores and hyphens, starting with a letter)", module, alt)
		}
	}

	force, _ := cmd.Flags().GetBool("force")
	entries, err := os.ReadDir(".")
	if err != nil {
		return projectTarget{}, err
	}
	var extra []string
	for _, e := range entries {
		if !inPlaceFresh.MatchString(e.Name()) {
			extra = append(extra, e.Name())
		}
	}
	if len(extra) > 0 && !force {
		if len(extra) > 5 {
			extra = append(extra[:5], "...")
		}
		return projectTarget{}, fmt.Errorf("the current directory already has project files (%s); "+
			"pass --force to generate here anyway (existing files are never overwritten)", strings.Join(extra, ", "))
	}

	// 就地生成一律保留既有檔案
	keepExisting = true
	scaffold.KeepExisting = true
	scaffold.OnKeep = reportKept
	scaffold.OnMerge = reportMerged
	return projectTarget{dir: ".", name: name, module: module, inPlace: true}, nil
}

// moduleBaseName 取 module 路徑的最後一段當專案名稱；結尾的主版本（/v2）不算
func moduleBaseName(module string) string {
	parts := strings.Split(module, "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && regexp.MustCompile(`^v[0-9]+$`).MatchString(last) {
		last = parts[len(parts)-2]
	}
	return last
}

// checkNewProject 建立專案前的共用檢查（hyp new 各類型與 hyp api）：名稱合法、目標目錄可用。
// 先前完全不檢查：hyp new ../escaped 寫到 CWD 之外並產生 module ../escaped，"my app" 產生 module my app；
// 對既有專案重跑 hyp new / hyp api 會截斷 go.mod、config.yaml 等既有檔案後才在半途失敗
func checkNewProject(cmd *cobra.Command, name string) error {
	if err := validateProjectName(name); err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	// --force：既有檔案保留並略過。先前遇到第一個既有檔案就中斷（GitHub 新建的 repo 通常已有
	// README.md、.gitignore），留下寫到一半、重跑卡在別的檔案而無法續完的專案
	keepExisting = force
	scaffold.KeepExisting = force
	scaffold.OnKeep = reportKept
	scaffold.OnMerge = reportMerged
	fi, err := os.Stat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s already exists and is not a directory", name)
	}
	entries, err := os.ReadDir(name)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !force {
		return fmt.Errorf("directory %s already exists and is not empty; choose another name or pass --force (existing files are never overwritten)", name)
	}
	return nil
}

// validateProjectName 專案名稱同時是目錄名稱與 go.mod 的 module 路徑：
// 必須是單一路徑元素，且是合法的 Go import path（不含空白、不以 . 開頭等）
func validateProjectName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid project name %q: must be a single directory name without path separators", name)
	}
	if err := module.CheckImportPath(name); err != nil {
		return fmt.Errorf("invalid project name %q: not a valid Go module path: %w", name, err)
	}
	// 名稱就是 module 路徑：與 go 指令的保留字或標準庫套件同名時專案建得出來，但一編譯就失敗
	//（"tool" is not an importable package / ambiguous import: found package log in multiple modules）
	if reservedModuleNames[strings.ToLower(name)] {
		return fmt.Errorf("invalid project name %q: it is reserved by the go command or is a standard library package, "+
			"so a module of that name cannot be built; choose another name (or go mod init a full module path and run hyp new without a name)", name)
	}
	return nil
}

// reservedModuleNames 不能當單段 module 路徑的名稱：go 指令的保留字與標準庫的頂層套件
var reservedModuleNames = func() map[string]bool {
	m := map[string]bool{}
	for _, s := range strings.Fields("all cmd std tool vendor " +
		"archive bufio builtin bytes cmp compress container context crypto database debug embed encoding errors expvar " +
		"flag fmt go hash html image index io iter log maps math mime net os path plugin reflect regexp runtime slices " +
		"sort strconv strings structs sync syscall testing text time unicode unique unsafe weak") {
		m[s] = true
	}
	return m
}()

// keepExisting 由 checkNewProject 依 --force 設定：為 true 時 writeNewFile 保留既有檔案並略過
var keepExisting bool

// reportKept 回報被保留的既有檔案（--force 或就地生成）
func reportKept(path string) {
	fmt.Printf("  = kept existing %s\n", filepath.ToSlash(path))
}

// reportMerged 回報既有 go.mod 補上了骨架需要的 require
func reportMerged(path string) {
	fmt.Printf("  ~ added requirements to %s\n", filepath.ToSlash(path))
}

// writeNewFile 建立新檔案，絕不截斷既有檔案：已存在時回錯，--force（keepExisting）時保留並略過。
// .go 檔先經 gofmt：先前 hyp api 的 auth_service.go、routers/auth.go 過不了 gofmt -l，
// 新專案的格式檢查一開始就失敗；模板產出不合法的 Go 時也在此回錯
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	if strings.HasSuffix(path, ".go") {
		formatted, err := format.Source(data)
		if err != nil {
			return fmt.Errorf("generated invalid Go for %s: %w", path, err)
		}
		data = formatted
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		if os.IsExist(err) {
			if keepExisting {
				// 既有的 go.mod 是使用者自己的：只補上骨架需要的 require
				if filepath.Base(path) == "go.mod" {
					merged, merr := scaffold.MergeGoMod(path, data)
					if merr != nil {
						return merr
					}
					if merged {
						reportMerged(path)
						return nil
					}
				}
				reportKept(path)
				return nil
			}
			return fmt.Errorf("file already exists: %s", path)
		}
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runNewCLI 生成 CLI 專案
func runNewCLI(t projectTarget) error {
	if err := scaffold.GenerateCLIProject(t.dir, t.name, t.module); err != nil {
		return fmt.Errorf("failed to create CLI project: %w", err)
	}

	warn := postScaffoldUpgrade(t.dir)
	defer printScaffoldWarning(warn)

	fmt.Printf("\n✨ Successfully created CLI project: %s\n", t.name)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", t.label())
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── commands/\n")
	fmt.Printf("   │   │   ├── root.go\n")
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol cli）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── tools/\n")
	fmt.Printf("   │   └── genctx/          # 產生 .hyp/context.yaml（go run ./tools/genctx）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	t.printCD()
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add subcommands:\n")
	fmt.Printf("   hyp generate command process\n")
	fmt.Printf("   hyp generate command export\n")
	fmt.Printf("\n🧩 Generate schema manifest (.hyp/context.yaml):\n")
	fmt.Printf("   go run ./tools/genctx\n")

	return nil
}

// runNewDesktop 生成 Desktop 專案（Fyne）。
// installDeps 為 true 時，缺少 C 工具鏈會代為執行平台的安裝命令。
func runNewDesktop(t projectTarget, installDeps bool) error {
	if err := scaffold.GenerateDesktopProject(t.dir, t.name, t.module); err != nil {
		return fmt.Errorf("failed to create desktop project: %w", err)
	}

	warn := postScaffoldUpgrade(t.dir)
	defer printScaffoldWarning(warn)

	fmt.Printf("\n✨ Successfully created Desktop project: %s\n", t.name)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", t.label())
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── views/\n")
	fmt.Printf("   │   │   ├── main_view.go\n")
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol desktop）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── tools/\n")
	fmt.Printf("   │   └── genctx/          # 產生 .hyp/context.yaml（go run ./tools/genctx）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	t.printCD()
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   go run .\n")
	fmt.Printf("\n📝 Add views:\n")
	fmt.Printf("   hyp generate view settings\n")
	fmt.Printf("   hyp generate view dashboard\n")
	fmt.Printf("\n🧩 Generate schema manifest (.hyp/context.yaml):\n")
	fmt.Printf("   go run ./tools/genctx\n")

	ensureDesktopDeps(installDeps)

	return nil
}

// runNewGRPC 生成 gRPC 微服務專案
func runNewGRPC(t projectTarget) error {
	if err := scaffold.GenerateGRPCProject(t.dir, t.name, t.module); err != nil {
		return fmt.Errorf("failed to create gRPC project: %w", err)
	}

	warn := postScaffoldUpgrade(t.dir)
	defer printScaffoldWarning(warn)

	// 套件與檔名用識別字形式（user-service → user_service）
	lowerName := strings.ToLower(scaffold.ProjectIdent(t.name))
	fmt.Printf("\n✨ Successfully created gRPC project: %s\n", t.name)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", t.label())
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── proto/%spb/\n", lowerName)
	fmt.Printf("   │   │   └── %s.proto\n", lowerName)
	fmt.Printf("   │   ├── rpc/\n")
	fmt.Printf("   │   │   ├── %s_server.go\n", lowerName)
	fmt.Printf("   │   │   └── schema.go    # RegisterSchemas（Protocol grpc）\n")
	fmt.Printf("   │   ├── models/\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml      # runtime 設定（main.go 載入）\n")
	fmt.Printf("   ├── main.go\n")
	fmt.Printf("   ├── Makefile\n")
	fmt.Printf("   └── go.mod\n")
	fmt.Printf("\n🚀 Get started:\n")
	t.printCD()
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   make proto              # compile .proto → Go code\n")
	fmt.Printf("   go run .                # 啟動時自動寫 .hyp/context.yaml（gRPC schema manifest）\n")
	fmt.Printf("\n📝 Add more services:\n")
	fmt.Printf("   hyp generate proto order\n")
	fmt.Printf("\n⚠️  Requires: protoc + protoc-gen-go + protoc-gen-go-grpc\n")
	fmt.Printf("   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest\n")
	fmt.Printf("   go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest\n")

	return nil
}

func runNew(t projectTarget) error {
	projectName := t.dir

	// 創建項目目錄結構
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "routers"), // 路由層置於專案根目錄（與 app/ 平行）
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, ".hyp"),
		filepath.Join(projectName, "logs"),
		filepath.Join(projectName, "static", "css"),
		filepath.Join(projectName, "static", "js"),
		filepath.Join(projectName, "static", "images"),
		filepath.Join(projectName, "templates"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// logs/ 是空目錄，git 不會追蹤；放 .gitkeep 讓 clone 後目錄仍存在，
	// logger 寫 logs/app.log 時不需先手動建目錄
	if err := createGitKeep(filepath.Join(projectName, "logs")); err != nil {
		return err
	}

	// 創建配置文件
	if err := createNewConfigFile(projectName); err != nil {
		return err
	}

	// 創建設計時設定檔（.hyp/llm.yaml + comment.yaml + config.yaml + crypt.yaml）
	if err := createLLMConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}
	if err := createCommentConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}
	if err := createHypConfigFile(filepath.Join(projectName, ".hyp")); err != nil {
		return err
	}
	if err := writeNewFile(filepath.Join(projectName, ".hyp", "crypt.yaml"), []byte(scaffold.CryptYamlTemplate), 0644); err != nil {
		return err
	}

	// @ai:generated 標記所用日期（YYYY-MM-DD）
	today := time.Now().Format("2006-01-02")

	// 創建示例 Model（Schema Input/Output DTO）
	if err := createHomeModel(projectName, today); err != nil {
		return err
	}

	// 創建示例 Controller（薄控制器，引用 model）
	if err := createFullStackController(projectName, t.module, today); err != nil {
		return err
	}

	// 創建 Router 入口（routers/router.go，引入 router + schema，串接 controller + model）
	if err := createRouterSetup(projectName, t.module, today); err != nil {
		return err
	}

	// 創建主程序文件（呼叫 routers.Setup）
	if err := createMainFile(projectName, t.module); err != nil {
		return err
	}

	// 創建歡迎頁面模板
	if err := createWelcomeTemplate(projectName); err != nil {
		return err
	}

	// 創建靜態資源
	if err := createStaticAssets(projectName); err != nil {
		return err
	}

	// 創建 go.mod
	if err := createGoMod(projectName, t.module); err != nil {
		return err
	}
	if err := writeNewFile(filepath.Join(projectName, ".gitignore"), []byte(scaffold.GitignoreTemplate), 0644); err != nil {
		return err
	}

	warn := postScaffoldUpgrade(projectName)
	defer printScaffoldWarning(warn)

	fmt.Printf("✨ Successfully created full-stack project: %s\n", t.name)
	fmt.Printf("📁 Project structure:\n")
	fmt.Printf("   %s/\n", t.label())
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/    # home.go（薄控制器）\n")
	fmt.Printf("   │   ├── models/         # home.go（Schema Input/Output DTO）\n")
	fmt.Printf("   │   └── services/\n")
	fmt.Printf("   ├── routers/            # router.go（Setup：router + schema 串接）\n")
	fmt.Printf("   ├── config/\n")
	fmt.Printf("   │   └── config.yaml\n")
	fmt.Printf("   ├── logs/               # .gitkeep（logger 輸出 logs/app.log）\n")
	fmt.Printf("   ├── static/\n")
	fmt.Printf("   │   ├── css/\n")
	fmt.Printf("   │   ├── js/\n")
	fmt.Printf("   │   └── images/\n")
	fmt.Printf("   ├── templates/\n")
	fmt.Printf("   │   ├── welcome.html\n")
	fmt.Printf("   │   └── templates.go   # embed 打包，routers.Setup 設定為樣板集\n")
	fmt.Printf("   ├── go.mod\n")
	fmt.Printf("   └── main.go\n")
	fmt.Printf("\n🚀 Get started:\n")
	t.printCD()
	fmt.Printf("   go mod tidy\n")
	fmt.Printf("   hyp run\n")

	return nil
}

func createNewConfigFile(projectName string) error {
	configContent := `# HypGo Configuration File

server:
  protocol: http2  # http1, http2, http3
  addr: :8080
  read_timeout: 30
  write_timeout: 30
  idle_timeout: 120
  max_handlers: 1000
  max_concurrent_streams: 100
  max_read_frame_size: 1048576
  enable_graceful_restart: true  # 啟用熱重啟
  # 可信代理網段：服務跑在 nginx / LB 之後時務必設定，否則 c.ClientIP()
  # 只回傳實際連線來源（安全預設），不會解析 X-Forwarded-For / X-Real-IP，
  # IPWhitelist / RateLimiter 等以 ClientIP 為 key 的功能會全部失準。
  # 支援 CIDR 或單一 IP；本機開發／服務直接暴露時留空即可。
  # trusted_proxies:
  #   - 127.0.0.1
  #   - 10.0.0.0/8
  # 預設中間件的 CORS：沒寫時允許任何來源（不帶憑證）。
  # 前端要帶 cookie 或想限制來源時列出來源；allow_origins: [] 表示不掛預設的 CORS。
  # cors:
  #   allow_origins: ["https://app.example.com"]
  #   allow_credentials: true
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

database:
  driver: mysql  # mysql, postgres, tidb, redis
  dsn: "user:password@tcp(localhost:3306)/database?charset=utf8mb4&parseTime=True&loc=Local"
  max_idle_conns: 10
  max_open_conns: 100
  redis:
    addr: "localhost:6379"
    password: ""
    db: 0

logger:
  level: debug  # debug, info, notice, warning, emergency
  output: logs/app.log  # stdout or file path
  colors: true  # only applied when writing to a terminal
  rotation:
    max_size: 100MB
    max_age: 7d  # backup retention (rounded up to days)
    max_backups: 10
    compress: true
`

	filename := filepath.Join(projectName, "config", "config.yaml")
	return writeNewFile(filename, []byte(configContent), 0644)
}

// createLLMConfigFile 寫入 llm.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.LLMYamlTemplate，預設 mode=none（不啟用 LLM）。
func createLLMConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "llm.yaml")
	return writeNewFile(filename, []byte(scaffold.LLMYamlTemplate), 0644)
}

// createCommentConfigFile 寫入 comment.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.CommentYamlTemplate，預設只開啟 normal_comment。
func createCommentConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "comment.yaml")
	return writeNewFile(filename, []byte(scaffold.CommentYamlTemplate), 0644)
}

// createHypConfigFile 寫入 .hyp/config.yaml 到指定目錄（預期為 .hyp/）。
// 內容來自 scaffold.HypConfigYamlTemplate，預設 language=en / debug_level=debug / environment=local。
func createHypConfigFile(configDir string) error {
	filename := filepath.Join(configDir, "config.yaml")
	return writeNewFile(filename, []byte(scaffold.HypConfigYamlTemplate), 0644)
}

// startupLineSnippet 是生成的 main.go 在 app.Run() 前印的一行。
// logger 預設寫到 logs/ 下的檔案：先前 go run . 後終端機完全沒有輸出，看不出服務是否啟動、日誌在哪
const startupLineSnippet = "\t// logger 預設寫檔，終端機另外印一行，方便確認位址與日誌位置\n" +
	"\tfmt.Printf(\"starting on %s (logs: %s)\\n\", app.Config().Server.Addr, app.Config().Logger.Output)\n\n"

func createMainFile(projectName, module string) error {
	mainContent := "package main\n\n" +
		"import (\n" +
		"\t\"fmt\"\n" +
		"\t\"os\"\n\n" +
		"\t\"github.com/maoxiaoyue/hypgo\"\n\n" +
		"\t\"" + module + "/routers\"\n" +
		")\n\n" +
		"// configPath 是 runtime 設定檔位置（專案根目錄 config/；.hyp/ 下的是設計時設定，兩者分離）\n" +
		"const configPath = \"config/config.yaml\"\n\n" +
		"func main() {\n" +
		"\t// 嚴格載入設定：檔案不存在或內容無效即結束，不靜默退回預設值起跑；\n" +
		"\t// logger 與 server 依 config 建立（logger.level / output / colors / rotation 都會生效）\n" +
		"\tapp, err := hypgo.NewWithConfig(configPath)\n" +
		"\tif err != nil {\n" +
		"\t\tfmt.Fprintf(os.Stderr, \"load %s: %v\\n\", configPath, err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n\n" +
		"\t// 所有路由與中間件定義於 routers/router.go\n" +
		"\trouters.Setup(app.Server().Router())\n\n" +
		startupLineSnippet +
		"\t// Run 會阻塞到收到 SIGINT/SIGTERM 並完成優雅關閉（Start() 內建，不必自己處理訊號）\n" +
		"\tif err := app.Run(); err != nil {\n" +
		"\t\tapp.Logger().Errorf(\"server error: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"}\n"

	filename := filepath.Join(projectName, "main.go")
	return writeNewFile(filename, []byte(mainContent), 0644)
}

// createHomeModel 生成 app/models/home.go，定義 Schema Input/Output 用的 DTO。
func createHomeModel(projectName, today string) error {
	content := "// Package models 定義資料模型與 Schema 的 Request/Response DTO。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package models\n\n" +
		"// HealthResp 健康檢查回應（Schema Output）。\n" +
		"type HealthResp struct {\n" +
		"\tSuccess bool   `json:\"success\"`\n" +
		"\tMessage string `json:\"message\"`\n" +
		"}\n\n" +
		"// InfoResp 伺服器資訊回應（Schema Output）。\n" +
		"type InfoResp struct {\n" +
		"\tSuccess bool       `json:\"success\"`\n" +
		"\tMessage string     `json:\"message\"`\n" +
		"\tData    InfoDetail `json:\"data\"`\n" +
		"}\n\n" +
		"// InfoDetail 伺服器資訊明細。\n" +
		"type InfoDetail struct {\n" +
		"\tVersion  string `json:\"version\"`\n" +
		"\tProtocol string `json:\"protocol\"`\n" +
		"}\n"

	filename := filepath.Join(projectName, "app", "models", "home.go")
	return writeNewFile(filename, []byte(content), 0644)
}

// createRouterSetup 生成 routers/router.go（專案根目錄）：引入 router + schema，
// 以 Schema-first 串接 controller 與 model，作為專案路由的唯一入口。
func createRouterSetup(projectName, module, today string) error {
	content := "// Package routers 集中定義路由與中間件（Schema-first MVC 的 Router 層）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package routers\n\n" +
		"import (\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/router\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/schema\"\n\n" +
		"\t\"" + module + "/app/controllers\"\n" +
		"\t\"" + module + "/app/models\"\n" +
		"\t\"" + module + "/templates\"\n" +
		")\n\n" +
		"// Setup 註冊所有路由與中間件。\n" +
		"// 在 main.go 呼叫：routers.Setup(app.Server().Router())。\n" +
		"func Setup(r *router.Router) {\n" +
		"\t// Recovery / Logger / RejectEarlyData / Security / CORS 由 server.Start() 自動套用（v0.8.11+），\n" +
		"\t// 不要再 r.Use(middleware.DefaultMiddleware()...)，否則每個請求會跑兩次。\n" +
		"\t// 額外的全域中間件（CSRF、BodyLimit…）在此加\n\n" +
		"\t// 靜態資源\n" +
		"\tr.Static(\"/static\", \"./static\")\n\n" +
		"\t// HTML 樣板（embed 打包，c.HTML 以檔名渲染）\n" +
		"\tr.SetHTMLTemplate(templates.HTML)\n\n" +
		"\t// Controller 實例（薄控制器；業務邏輯置於 app/services）\n" +
		"\thome := &controllers.HomeController{}\n\n" +
		// 先前首頁以 r.GET 註冊、沒有 Schema：全新專案的 hyp lint 即因 no_schema 失敗
		"\t// 首頁（回傳 HTML 檔案：沒有 JSON Output 型別，以 OutputName 標示內容型態，contract 測試不驗 body）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:     \"GET\",\n" +
		"\t\tPath:       \"/\",\n" +
		"\t\tSummary:    \"Welcome page\",\n" +
		"\t\tTags:       []string{\"web\"},\n" +
		"\t\tOutputName: \"text/html\",\n" +
		"\t}).Handle(home.Home)\n\n" +
		"\t// API（Schema-first：宣告 Input/Output 型別，供 c.BindInput 與 contract 測試對齊）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/api/health\",\n" +
		"\t\tSummary: \"Health check\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.HealthResp{},\n" +
		"\t}).Handle(home.Health)\n\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/api/info\",\n" +
		"\t\tSummary: \"Server info\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.InfoResp{},\n" +
		"\t}).Handle(home.Info)\n\n" +
		"\t// 在此註冊更多資源路由，例如由 `hyp generate controller user` 產生的：\n" +
		"\t// RegisterUserRoutes(r)\n" +
		"}\n"

	filename := filepath.Join(projectName, "routers", "router.go")
	return writeNewFile(filename, []byte(content), 0644)
}

// createGitKeep 在空目錄放 .gitkeep，讓該目錄能被 git 追蹤（hyp new / hyp api 共用）
func createGitKeep(dir string) error {
	return writeNewFile(filepath.Join(dir, ".gitkeep"), nil, 0644)
}

func createFullStackController(projectName, module, today string) error {
	controllerContent := "// Package controllers 承載 HTTP handler（薄控制器：解析輸入 → 呼叫 service → 寫回應）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package controllers\n\n" +
		"import (\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n\n" +
		"\t\"" + module + "/app/models\"\n" +
		")\n\n" +
		"// HomeController 處理首頁與系統狀態相關路由。\n" +
		"type HomeController struct{}\n\n" +
		"// Home 回傳歡迎頁面（templates/welcome.html，經 routers.Setup 設定的樣板集渲染）。\n" +
		"func (ctrl *HomeController) Home(c *hypcontext.Context) {\n" +
		"\tc.HTML(200, \"welcome.html\", map[string]string{\n" +
		"\t\t\"Title\":    \"Welcome to HypGo\",\n" +
		"\t\t\"Protocol\": c.Request.Proto,\n" +
		"\t})\n" +
		"}\n\n" +
		"// Health 回傳服務健康狀態。\n" +
		"func (ctrl *HomeController) Health(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.HealthResp{\n" +
		"\t\tSuccess: true,\n" +
		"\t\tMessage: \"Server is healthy\",\n" +
		"\t})\n" +
		"}\n\n" +
		"// Info 回傳伺服器資訊。\n" +
		"func (ctrl *HomeController) Info(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.InfoResp{\n" +
		"\t\tSuccess: true,\n" +
		"\t\tMessage: \"HypGo Framework\",\n" +
		"\t\tData: models.InfoDetail{\n" +
		"\t\t\tVersion:  \"" + Version + "\",\n" +
		"\t\t\tProtocol: c.Request.Proto,\n" +
		"\t\t},\n" +
		"\t})\n" +
		"}\n"

	filename := filepath.Join(projectName, "app", "controllers", "home.go")
	return writeNewFile(filename, []byte(controllerContent), 0644)
}

func createWelcomeTemplate(projectName string) error {
	templateContent := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <link rel="stylesheet" href="/static/css/style.css">
</head>
<body>
    <div class="container">
        <div class="hero">
            <h1 class="title">Welcome to HypGo</h1>
            <p class="subtitle">A Modern Go Web Framework with HTTP/3 Support</p>
            <div class="protocol-badge">{{.Protocol}}</div>
        </div>

        <div class="features">
            <div class="feature-card">
                <div class="feature-icon">🚀</div>
                <h3>HTTP/3 Ready</h3>
                <p>Built-in support for HTTP/3.0, HTTP/2.0, and HTTP/1.1</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">💾</div>
                <h3>Multiple Databases</h3>
                <p>MySQL, PostgreSQL, TiDB, Redis, and Cassandra support</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">⚡</div>
                <h3>WebSocket</h3>
                <p>Real-time communication with built-in WebSocket support</p>
            </div>
            <div class="feature-card">
                <div class="feature-icon">🔥</div>
                <h3>Hot Reload</h3>
                <p>Development mode with automatic code reloading</p>
            </div>
        </div>

        <div class="actions">
            <button id="testApi" class="btn btn-primary">Test API</button>
            <button id="connectWs" class="btn btn-secondary">Connect WebSocket</button>
        </div>

        <div id="output" class="output"></div>

        <div class="footer">
            <p>HypGo Framework &copy; 2024 | <a href="https://github.com/maoxiaoyue/hypgo">GitHub</a></p>
        </div>
    </div>

    <script src="/static/js/app.js"></script>
</body>
</html>
`

	filename := filepath.Join(projectName, "templates", "welcome.html")
	if err := writeNewFile(filename, []byte(templateContent), 0644); err != nil {
		return err
	}
	return writeNewFile(filepath.Join(projectName, "templates", "templates.go"), []byte(templatesPackage), 0644)
}

// templatesPackage 以 embed 打包 templates/*.html。先前首頁以相對路徑 c.File("templates/welcome.html")
// 送出：工作目錄不是專案根目錄時（從 routers/ 執行的 contract 測試、從別處啟動的伺服器）一律 404，
// 且 welcome.html 的 {{.Title}} / {{.Protocol}} 從未經過樣板渲染、原樣送出
const templatesPackage = `// Package templates 以 embed 打包 HTML 樣板，渲染不依賴執行時的工作目錄。
package templates

import (
	"embed"
	"html/template"
)

//go:embed *.html
var files embed.FS

// HTML 為已解析的樣板集，由 routers.Setup 交給 r.SetHTMLTemplate
var HTML = template.Must(template.ParseFS(files, "*.html"))
`

func createStaticAssets(projectName string) error {
	// CSS 文件
	cssContent := `* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    min-height: 100vh;
    color: #333;
}

.container {
    max-width: 1200px;
    margin: 0 auto;
    padding: 2rem;
}

.hero {
    text-align: center;
    padding: 4rem 0;
    color: white;
}

.title {
    font-size: 4rem;
    font-weight: 700;
    margin-bottom: 1rem;
    text-shadow: 2px 2px 4px rgba(0,0,0,0.3);
}

.subtitle {
    font-size: 1.5rem;
    opacity: 0.9;
    margin-bottom: 2rem;
}

.protocol-badge {
    display: inline-block;
    background: rgba(255,255,255,0.2);
    padding: 0.5rem 1.5rem;
    border-radius: 25px;
    font-weight: 600;
    backdrop-filter: blur(10px);
}

.features {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
    gap: 2rem;
    margin: 3rem 0;
}

.feature-card {
    background: white;
    padding: 2rem;
    border-radius: 15px;
    box-shadow: 0 10px 30px rgba(0,0,0,0.1);
    text-align: center;
    transition: transform 0.3s ease;
}

.feature-card:hover {
    transform: translateY(-5px);
}

.feature-icon {
    font-size: 3rem;
    margin-bottom: 1rem;
}

.feature-card h3 {
    margin-bottom: 0.5rem;
    color: #667eea;
}

.actions {
    text-align: center;
    margin: 3rem 0;
}

.btn {
    padding: 0.75rem 2rem;
    font-size: 1rem;
    border: none;
    border-radius: 25px;
    cursor: pointer;
    margin: 0 0.5rem;
    transition: all 0.3s ease;
    font-weight: 600;
}

.btn-primary {
    background: #667eea;
    color: white;
}

.btn-primary:hover {
    background: #5a67d8;
    transform: scale(1.05);
}

.btn-secondary {
    background: #48bb78;
    color: white;
}

.btn-secondary:hover {
    background: #38a169;
    transform: scale(1.05);
}

.output {
    background: white;
    border-radius: 10px;
    padding: 1.5rem;
    margin: 2rem 0;
    min-height: 100px;
    box-shadow: 0 5px 20px rgba(0,0,0,0.1);
    display: none;
}

.output.show {
    display: block;
}

.output pre {
    background: #f7fafc;
    padding: 1rem;
    border-radius: 5px;
    overflow-x: auto;
}

.footer {
    text-align: center;
    padding: 2rem 0;
    color: white;
}

.footer a {
    color: white;
    text-decoration: underline;
}
`

	cssFile := filepath.Join(projectName, "static", "css", "style.css")
	if err := writeNewFile(cssFile, []byte(cssContent), 0644); err != nil {
		return err
	}

	// JavaScript 文件
	jsContent := `document.addEventListener('DOMContentLoaded', function() {
    const output = document.getElementById('output');
    const testApiBtn = document.getElementById('testApi');
    const connectWsBtn = document.getElementById('connectWs');
    
    let ws = null;

    // 測試 API
    testApiBtn.addEventListener('click', async function() {
        try {
            const response = await fetch('/api/info');
            const data = await response.json();
            
            output.classList.add('show');
            output.innerHTML = '<h3>API Response:</h3><pre>' + JSON.stringify(data, null, 2) + '</pre>';
        } catch (error) {
            output.classList.add('show');
            output.innerHTML = '<h3>Error:</h3><pre>' + error.message + '</pre>';
        }
    });

    // WebSocket 連接
    connectWsBtn.addEventListener('click', function() {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.close();
            connectWsBtn.textContent = 'Connect WebSocket';
            output.innerHTML += '<p>WebSocket disconnected</p>';
            return;
        }

        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(protocol + '//' + window.location.host + '/ws');

        ws.onopen = function() {
            connectWsBtn.textContent = 'Disconnect WebSocket';
            output.classList.add('show');
            output.innerHTML = '<h3>WebSocket Connected</h3>';
            
            // 發送測試消息
            ws.send(JSON.stringify({
                type: 'subscribe',
                data: { channel: 'test' }
            }));
        };

        ws.onmessage = function(event) {
            const data = JSON.parse(event.data);
            output.innerHTML += '<p>Received: ' + JSON.stringify(data) + '</p>';
        };

        ws.onerror = function(error) {
            output.innerHTML += '<p>Error: ' + error + '</p>';
        };

        ws.onclose = function() {
            connectWsBtn.textContent = 'Connect WebSocket';
            output.innerHTML += '<p>WebSocket closed</p>';
        };
    });
});
`

	jsFile := filepath.Join(projectName, "static", "js", "app.js")
	return writeNewFile(jsFile, []byte(jsContent), 0644)
}

func createGoMod(projectName, module string) error {
	// 版本取自 hyp 本身（version.go），postScaffoldUpgrade 再嘗試升到 @latest。
	// 先前以 git ls-remote git@github.com:...（SSH，沒有金鑰即失敗）查最新 tag，失敗時退回 v0.8.5——
	// 該版本沒有根 hypgo 套件，離線或 proxy 失敗時產生的專案永遠編不過
	content := fmt.Sprintf(`module %s

go 1.24

require github.com/maoxiaoyue/hypgo v%s
`, module, Version)

	filename := filepath.Join(projectName, "go.mod")
	return writeNewFile(filename, []byte(content), 0644)
}
