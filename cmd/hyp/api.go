// @chris
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/scaffold"
	"github.com/spf13/cobra"
)

// llmYamlContent 為 hyp api 生成 .hyp/llm.yaml 時採用的預設內容。
// 直接複用 scaffold 套件的共用模板，確保所有專案模式內容一致。
var llmYamlContent = scaffold.LLMYamlTemplate

// commentYamlContent 為 hyp api 生成 .hyp/comment.yaml 時採用的預設內容。
var commentYamlContent = scaffold.CommentYamlTemplate

// hypConfigYamlContent 為 hyp api 生成 .hyp/config.yaml 時採用的預設內容。
var hypConfigYamlContent = scaffold.HypConfigYamlTemplate

var apiCmd = &cobra.Command{
	Use:   "api [project-name]",
	Short: "Create a new HypGo API-only project with HTTP/3 support",
	Args:  cobra.ExactArgs(1),
	RunE:  runAPI,
}

func init() {
	rootCmd.AddCommand(apiCmd)
}

func runAPI(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// 創建 API 項目目錄結構
	// 與 hyp new 同構（app/{controllers,models,services} + routers/），
	// 不再產生自製的 internal/{logger,database,cache}：框架的 pkg/logger、
	// pkg/hidb 已涵蓋，重造一份只會與框架 API 漂移（舊版即因此編不過）
	dirs := []string{
		filepath.Join(projectName, "app", "controllers"),
		filepath.Join(projectName, "app", "models"),
		filepath.Join(projectName, "app", "services"),
		filepath.Join(projectName, "routers"), // 路由層置於專案根目錄（與 app/ 平行）
		filepath.Join(projectName, "config"),
		filepath.Join(projectName, ".hyp"),
		filepath.Join(projectName, "migrations"),
		filepath.Join(projectName, "tests"),
		filepath.Join(projectName, "docs"),
		filepath.Join(projectName, "logs"),
		filepath.Join(projectName, "certs"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// logs/ 為空目錄，放 .gitkeep 讓 clone 後仍存在（.gitignore 已排除 logs/* 但保留 .gitkeep）
	if err := createGitKeep(filepath.Join(projectName, "logs")); err != nil {
		return err
	}

	// 創建所有非 Go 檔案（設定、部署、遷移）
	files := []fileTemplate{
		{Path: "config/config.yaml", Content: configYamlContent},
		{Path: ".hyp/llm.yaml", Content: llmYamlContent},
		{Path: ".hyp/comment.yaml", Content: commentYamlContent},
		{Path: ".hyp/config.yaml", Content: hypConfigYamlContent},
		{Path: ".env.example", Content: envExampleContent},

		// 部署和配置
		{Path: "Dockerfile", Content: dockerfileContent},
		{Path: "docker-compose.yml", Content: dockerComposeContent},
		{Path: "Makefile", Content: makefileContent},
		{Path: ".gitignore", Content: gitignoreContent},
		{Path: ".air.toml", Content: airTomlContent},
		{Path: "README.md", Content: readmeContent},
		{Path: "go.mod", Content: goModContent},

		// 數據庫遷移
		{Path: "migrations/001_create_users.up.sql", Content: createUsersUpSQL},
		{Path: "migrations/001_create_users.down.sql", Content: createUsersDownSQL},
		{Path: "migrations/002_create_roles.up.sql", Content: createRolesUpSQL},
		{Path: "migrations/002_create_roles.down.sql", Content: createRolesDownSQL},
	}

	// 模板數據
	data := map[string]string{
		"ProjectName": projectName,
	}

	// 創建所有檔案
	for _, file := range files {
		fullPath := filepath.Join(projectName, file.Path)
		if err := createTemplateFile(fullPath, file.Content, data); err != nil {
			return fmt.Errorf("failed to create %s: %w", file.Path, err)
		}
	}

	today := time.Now().Format("2006-01-02")

	// Go 程式碼一律走 pkg/scaffold 的生成器（與 hyp generate 同一套、有測試覆蓋），
	// 保證產出對齊框架真實 API；user 資源 = model + service + controller + Schema 路由
	const resource = "user"
	gen := []struct {
		label string
		fn    func() error
	}{
		{"app/models/user.go", func() error { return scaffold.GenerateModel(filepath.Join(projectName, "app", "models"), resource) }},
		{"app/services/user_service.go", func() error { return scaffold.GenerateService(filepath.Join(projectName, "app", "services"), resource) }},
		{"app/controllers/user_controller.go", func() error {
			return scaffold.GenerateController(filepath.Join(projectName, "app", "controllers"), resource, projectName)
		}},
		{"routers/user.go", func() error {
			return scaffold.GenerateRouter(filepath.Join(projectName, "routers"), resource, projectName)
		}},
		{"routers/middleware.go", func() error { return scaffold.GenerateMiddleware(filepath.Join(projectName, "routers")) }},
		{"app/models/health.go + app/controllers/health.go", func() error { return createAPIHealth(projectName, today) }},
		{"auth（models/auth.go, services/auth_service.go, controllers/auth_controller.go, routers/auth.go）", func() error {
			return createAPIAuth(projectName, today)
		}},
		{"routers/router.go", func() error { return createAPIRouterSetup(projectName, today) }},
		{"main.go", func() error { return createAPIMainFile(projectName) }},
	}
	for _, g := range gen {
		if err := g.fn(); err != nil {
			return fmt.Errorf("failed to create %s: %w", g.label, err)
		}
	}

	// 把生成專案的 hypgo 依賴升到 @latest（如果可以）
	postScaffoldUpgrade(projectName)

	// 打印成功信息
	printSuccessMessage(projectName)

	return nil
}

// createAPIRouterSetup 生成 routers/router.go（專案根目錄）：全域中間件 +
// /health + RegisterUserRoutes（由 scaffold.GenerateRouter 產生），
// 作為 API 專案路由的唯一入口；main.go 以 routers.Setup(...) 呼叫。
func createAPIRouterSetup(projectName, today string) error {
	content := "// Package routers 集中定義路由與中間件（Schema-first MVC 的 Router 層）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package routers\n\n" +
		"import (\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/router\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/schema\"\n\n" +
		"\t\"" + projectName + "/app/controllers\"\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		")\n\n" +
		"// Setup 註冊所有路由與中間件。\n" +
		"// 在 main.go 呼叫：routers.Setup(app.Server().Router())。\n" +
		"func Setup(r *router.Router) {\n" +
		"\t// Recovery / Logger / Security / CORS 由 server.Start() 自動套用（v0.8.11+），\n" +
		"\t// 不要再 r.Use(middleware.DefaultMiddleware()...)，否則每個請求會跑兩次。\n" +
		"\t// 額外的全域中間件（JWT、RateLimiter、BodyLimit…）在此加，或用 routers/middleware.go 的 APIMiddleware()\n\n" +
		"\t// 健康檢查（Schema-first：宣告 Output 供 contract 測試與 manifest 使用）\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/health\",\n" +
		"\t\tSummary: \"Health check\",\n" +
		"\t\tTags:    []string{\"system\"},\n" +
		"\t\tOutput:  models.HealthResp{},\n" +
		"\t}).Handle(controllers.HealthCheck)\n\n" +
		"\t// 認證：/api/auth/register、/api/auth/login（公開）、/api/auth/me（需 Bearer token）\n" +
		"\tRegisterAuthRoutes(r)\n\n" +
		"\t// 資源路由（各資源一檔，由 hyp generate controller <name> 產生後在此註冊）\n" +
		"\tRegisterUserRoutes(r)\n" +
		"}\n"

	filename := filepath.Join(projectName, "routers", "router.go")
	return os.WriteFile(filename, []byte(content), 0644)
}

// createAPIAuth 生成 JWT 認證層：以框架的 middleware.JWT + HS256 簽發／驗證，
// bcrypt 雜湊密碼，預設記憶體帳號儲存（接 hidb 時實作 AccountStore 替換）
func createAPIAuth(projectName, today string) error {
	header := func(pkg, doc string) string {
		return "// Package " + pkg + " " + doc + "\n//\n// @ai:generated by=hypgo date=" + today + "\npackage " + pkg + "\n\n"
	}

	model := header("models", "定義資料模型與 Schema 的 Request/Response DTO。") +
		"// RegisterReq 註冊請求（Schema Input）；validate 由 c.BindInput 執行\n" +
		"type RegisterReq struct {\n" +
		"\tEmail    string `json:\"email\" validate:\"required,email\"`\n" +
		"\tPassword string `json:\"password\" validate:\"required,min=8,max=72\"` // bcrypt 上限 72 bytes\n" +
		"}\n\n" +
		"// LoginReq 登入請求（Schema Input）\n" +
		"type LoginReq struct {\n" +
		"\tEmail    string `json:\"email\" validate:\"required,email\"`\n" +
		"\tPassword string `json:\"password\" validate:\"required\"`\n" +
		"}\n\n" +
		"// TokenResp 登入成功回應（Schema Output）\n" +
		"type TokenResp struct {\n" +
		"\tToken     string `json:\"token\"`      // Bearer token（HS256）\n" +
		"\tExpiresAt int64  `json:\"expires_at\"` // Unix 秒\n" +
		"}\n\n" +
		"// MeResp 目前登入者（Schema Output）\n" +
		"type MeResp struct {\n" +
		"\tID    string   `json:\"id\"`\n" +
		"\tEmail string   `json:\"email\"`\n" +
		"\tRoles []string `json:\"roles\"`\n" +
		"}\n\n" +
		"// AdminStatsResp 管理端統計（Schema Output；需 admin 角色）\n" +
		"type AdminStatsResp struct {\n" +
		"\tAccounts int `json:\"accounts\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(projectName, "app", "models", "auth.go"), []byte(model), 0644); err != nil {
		return err
	}

	service := header("services", "提供業務邏輯層。") +
		"import (\n" +
		"\t\"context\"\n" +
		"\t\"strconv\"\n" +
		"\t\"sync\"\n" +
		"\t\"time\"\n\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/errors\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/middleware\"\n" +
		"\t\"golang.org/x/crypto/bcrypt\"\n" +
		")\n\n" +
		"// 認證錯誤（Error Catalog）\n" +
		"var (\n" +
		"\tErrAuthEmailTaken   = errors.Define(\"E_auth_001\", 409, \"Email already registered\", \"auth\")\n" +
		"\tErrAuthInvalidLogin = errors.Define(\"E_auth_002\", 401, \"Invalid email or password\", \"auth\")\n" +
		"\tErrAuthUnauthorized = errors.Define(\"E_auth_003\", 401, \"Authentication required\", \"auth\")\n" +
		"\tErrAuthInternal     = errors.Define(\"E_auth_004\", 500, \"Authentication failed\", \"auth\")\n" +
		"\tErrAuthForbidden    = errors.Define(\"E_auth_005\", 403, \"Insufficient permissions\", \"auth\")\n" +
		")\n\n" +
		"// RoleAdmin 管理者角色；第一個註冊的帳號自動取得（bootstrap），之後由管理者指派\n" +
		"const RoleAdmin = \"admin\"\n\n" +
		"// Account 帳號紀錄。接資料庫時改為 Bun model（bun.BaseModel + table tag）\n" +
		"type Account struct {\n" +
		"\tID           string\n" +
		"\tEmail        string\n" +
		"\tPasswordHash []byte\n" +
		"\tRoles        []string\n" +
		"}\n\n" +
		"// AccountStore 帳號儲存介面；預設 MemoryAccountStore，接 hidb 時實作同介面替換：\n" +
		"//   services.NewAuthService(&PgAccountStore{db: db}, secret, ttl)\n" +
		"type AccountStore interface {\n" +
		"\tFindByEmail(ctx context.Context, email string) (*Account, bool)\n" +
		"\tCreate(ctx context.Context, a *Account) error\n" +
		"\tCount(ctx context.Context) int\n" +
		"}\n\n" +
		"// MemoryAccountStore 行程內記憶體實作——重啟即清空，僅供開發／測試\n" +
		"type MemoryAccountStore struct {\n" +
		"\tmu      sync.RWMutex\n" +
		"\tbyEmail map[string]*Account\n" +
		"\tseq     int64\n" +
		"}\n\n" +
		"// NewMemoryAccountStore 建立空的記憶體帳號儲存\n" +
		"func NewMemoryAccountStore() *MemoryAccountStore {\n" +
		"\treturn &MemoryAccountStore{byEmail: make(map[string]*Account)}\n" +
		"}\n\n" +
		"// FindByEmail 依 email 查帳號\n" +
		"func (s *MemoryAccountStore) FindByEmail(_ context.Context, email string) (*Account, bool) {\n" +
		"\ts.mu.RLock()\n" +
		"\tdefer s.mu.RUnlock()\n" +
		"\ta, ok := s.byEmail[email]\n" +
		"\treturn a, ok\n" +
		"}\n\n" +
		"// Create 新增帳號並配發遞增 ID\n" +
		"func (s *MemoryAccountStore) Create(_ context.Context, a *Account) error {\n" +
		"\ts.mu.Lock()\n" +
		"\tdefer s.mu.Unlock()\n" +
		"\ts.seq++\n" +
		"\ta.ID = strconv.FormatInt(s.seq, 10)\n" +
		"\ts.byEmail[a.Email] = a\n" +
		"\treturn nil\n" +
		"}\n\n" +
		"// Count 帳號總數\n" +
		"func (s *MemoryAccountStore) Count(_ context.Context) int {\n" +
		"\ts.mu.RLock()\n" +
		"\tdefer s.mu.RUnlock()\n" +
		"\treturn len(s.byEmail)\n" +
		"}\n\n" +
		"// AuthService 註冊／登入／簽發 token\n" +
		"type AuthService struct {\n" +
		"\tstore  AccountStore\n" +
		"\tsecret []byte\n" +
		"\tttl    time.Duration\n" +
		"}\n\n" +
		"// NewAuthService 建立認證服務；secret 為 HS256 金鑰、ttl 為 token 有效期\n" +
		"func NewAuthService(store AccountStore, secret []byte, ttl time.Duration) *AuthService {\n" +
		"\treturn &AuthService{store: store, secret: secret, ttl: ttl}\n" +
		"}\n\n" +
		"// Register 建立帳號（bcrypt 雜湊密碼）；email 重複回 ErrAuthEmailTaken\n" +
		"func (s *AuthService) Register(ctx context.Context, email, password string) (*Account, *errors.AppError) {\n" +
		"\tif _, exists := s.store.FindByEmail(ctx, email); exists {\n" +
		"\t\treturn nil, ErrAuthEmailTaken\n" +
		"\t}\n" +
		"\thash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)\n" +
		"\tif err != nil {\n" +
		"\t\treturn nil, ErrAuthInternal.With(\"reason\", err.Error())\n" +
		"\t}\n" +
		"\tacc := &Account{Email: email, PasswordHash: hash}\n" +
		"\t// bootstrap：第一個帳號即管理者，之後的角色由管理者指派\n" +
		"\tif s.store.Count(ctx) == 0 {\n" +
		"\t\tacc.Roles = []string{RoleAdmin}\n" +
		"\t}\n" +
		"\tif err := s.store.Create(ctx, acc); err != nil {\n" +
		"\t\treturn nil, ErrAuthInternal.With(\"reason\", err.Error())\n" +
		"\t}\n" +
		"\treturn acc, nil\n" +
		"}\n\n" +
		"// AccountCount 帳號總數（管理端統計）\n" +
		"func (s *AuthService) AccountCount(ctx context.Context) int {\n" +
		"\treturn s.store.Count(ctx)\n" +
		"}\n\n" +
		"// Login 驗證密碼並簽發 HS256 token（sub = 帳號 ID，roles = 角色，data.email = email）\n" +
		"func (s *AuthService) Login(ctx context.Context, email, password string) (token string, expiresAt int64, appErr *errors.AppError) {\n" +
		"\tacc, ok := s.store.FindByEmail(ctx, email)\n" +
		"\t// 帳號不存在時仍跑一次 bcrypt，避免以回應時間差探測 email 是否已註冊\n" +
		"\tif !ok {\n" +
		"\t\t_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))\n" +
		"\t\treturn \"\", 0, ErrAuthInvalidLogin\n" +
		"\t}\n" +
		"\tif err := bcrypt.CompareHashAndPassword(acc.PasswordHash, []byte(password)); err != nil {\n" +
		"\t\treturn \"\", 0, ErrAuthInvalidLogin\n" +
		"\t}\n\n" +
		"\tclaims := middleware.JWTClaims{\n" +
		"\t\tSubject: acc.ID,\n" +
		"\t\tRoles:   acc.Roles, // JWT 中間件會自動 c.SetRoles，供 middleware.RequireRole 使用\n" +
		"\t\tData:    map[string]interface{}{\"email\": acc.Email},\n" +
		"\t}\n" +
		"\ttoken, err := middleware.SignHS256(claims, s.secret, s.ttl)\n" +
		"\tif err != nil {\n" +
		"\t\treturn \"\", 0, ErrAuthInternal.With(\"reason\", err.Error())\n" +
		"\t}\n" +
		"\treturn token, time.Now().Add(s.ttl).Unix(), nil\n" +
		"}\n\n" +
		"// Validator 供 middleware.JWTConfig.Validator 使用（HS256，與 Login 同一把 secret）\n" +
		"func (s *AuthService) Validator() func(token string) (interface{}, error) {\n" +
		"\treturn middleware.HS256Validator(s.secret)\n" +
		"}\n\n" +
		"// dummyHash 供「帳號不存在」路徑做等時間比對\n" +
		"var dummyHash, _ = bcrypt.GenerateFromPassword([]byte(\"hypgo-dummy-password\"), bcrypt.DefaultCost)\n"
	if err := os.WriteFile(filepath.Join(projectName, "app", "services", "auth_service.go"), []byte(service), 0644); err != nil {
		return err
	}

	controller := header("controllers", "承載 HTTP handler（薄控制器：解析輸入 → 呼叫 service → 寫回應）。") +
		"import (\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/errors\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/middleware\"\n\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		"\t\"" + projectName + "/app/services\"\n" +
		")\n\n" +
		"// AuthController 註冊／登入／查詢目前登入者\n" +
		"type AuthController struct {\n" +
		"\tAuth *services.AuthService\n" +
		"}\n\n" +
		"// Register POST /api/auth/register\n" +
		"func (ctrl *AuthController) Register(c *hypcontext.Context) {\n" +
		"\tvar req models.RegisterReq\n" +
		"\tif !c.BindInput(&req) {\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"\tacc, appErr := ctrl.Auth.Register(c, req.Email, req.Password)\n" +
		"\tif appErr != nil {\n" +
		"\t\terrors.AbortWithAppError(c, appErr)\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"\tc.JSON(201, models.MeResp{ID: acc.ID, Email: acc.Email, Roles: nonNil(acc.Roles)})\n" +
		"}\n\n" +
		"// Login POST /api/auth/login\n" +
		"func (ctrl *AuthController) Login(c *hypcontext.Context) {\n" +
		"\tvar req models.LoginReq\n" +
		"\tif !c.BindInput(&req) {\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"\ttoken, exp, appErr := ctrl.Auth.Login(c, req.Email, req.Password)\n" +
		"\tif appErr != nil {\n" +
		"\t\terrors.AbortWithAppError(c, appErr)\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"\tc.JSON(200, models.TokenResp{Token: token, ExpiresAt: exp})\n" +
		"}\n\n" +
		"// Me GET /api/auth/me（需 Authorization: Bearer <token>；claims 由 JWT 中間件放入 context）\n" +
		"func (ctrl *AuthController) Me(c *hypcontext.Context) {\n" +
		"\tclaims, ok := middleware.JWTClaimsFrom(c, \"\")\n" +
		"\tif !ok {\n" +
		"\t\terrors.AbortWithAppError(c, services.ErrAuthUnauthorized)\n" +
		"\t\treturn\n" +
		"\t}\n" +
		"\temail, _ := claims.Data[\"email\"].(string)\n" +
		"\tc.JSON(200, models.MeResp{ID: claims.Subject, Email: email, Roles: nonNil(claims.Roles)})\n" +
		"}\n\n" +
		"// AdminStats GET /api/auth/admin/stats（需 admin 角色；由 middleware.RequireRole 把關）\n" +
		"func (ctrl *AuthController) AdminStats(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.AdminStatsResp{Accounts: ctrl.Auth.AccountCount(c)})\n" +
		"}\n\n" +
		"// nonNil 讓 JSON 輸出 [] 而非 null\n" +
		"func nonNil(s []string) []string {\n" +
		"\tif s == nil {\n" +
		"\t\treturn []string{}\n" +
		"\t}\n" +
		"\treturn s\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(projectName, "app", "controllers", "auth_controller.go"), []byte(controller), 0644); err != nil {
		return err
	}

	routes := header("routers", "集中定義路由與中間件（Schema-first MVC 的 Router 層）。") +
		"import (\n" +
		"\t\"crypto/rand\"\n" +
		"\t\"fmt\"\n" +
		"\t\"os\"\n" +
		"\t\"time\"\n\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/errors\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/middleware\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/router\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/schema\"\n\n" +
		"\t\"" + projectName + "/app/controllers\"\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		"\t\"" + projectName + "/app/services\"\n" +
		")\n\n" +
		"// RegisterAuthRoutes 註冊認證路由：\n" +
		"//   POST /api/auth/register、POST /api/auth/login（公開）\n" +
		"//   GET  /api/auth/me（Group 掛 middleware.JWT，Validator 為框架的 HS256）\n" +
		"//   GET  /api/auth/admin/stats（再掛 middleware.RequireRole(\"admin\")）\n" +
		"func RegisterAuthRoutes(r *router.Router) {\n" +
		"\tauth := services.NewAuthService(services.NewMemoryAccountStore(), jwtSecret(), 24*time.Hour)\n" +
		"\tctrl := &controllers.AuthController{Auth: auth}\n\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"POST\",\n" +
		"\t\tPath:    \"/api/auth/register\",\n" +
		"\t\tSummary: \"Register\",\n" +
		"\t\tTags:    []string{\"auth\"},\n" +
		"\t\tInput:   models.RegisterReq{},\n" +
		"\t\tOutput:  models.MeResp{},\n" +
		"\t\tResponses: map[int]schema.ResponseSchema{\n" +
		"\t\t\t201: {Description: \"Account created\"},\n" +
		"\t\t\t409: {Description: \"Email already registered\"},\n" +
		"\t\t\t422: {Description: \"Validation failed\"},\n" +
		"\t\t},\n" +
		"\t}).Handle(ctrl.Register)\n\n" +
		"\tr.Schema(schema.Route{\n" +
		"\t\tMethod:  \"POST\",\n" +
		"\t\tPath:    \"/api/auth/login\",\n" +
		"\t\tSummary: \"Login\",\n" +
		"\t\tTags:    []string{\"auth\"},\n" +
		"\t\tInput:   models.LoginReq{},\n" +
		"\t\tOutput:  models.TokenResp{},\n" +
		"\t\tResponses: map[int]schema.ResponseSchema{\n" +
		"\t\t\t200: {Description: \"Bearer token\"},\n" +
		"\t\t\t401: {Description: \"Invalid email or password\"},\n" +
		"\t\t},\n" +
		"\t}).Handle(ctrl.Login)\n\n" +
		"\t// 需登入的路由都掛在這個 Group 下；其他資源要保護時同樣用 protected.Schema(...)\n" +
		"\tprotected := r.NewGroup(\"/api/auth\", middleware.JWT(middleware.JWTConfig{\n" +
		"\t\tValidator: auth.Validator(),\n" +
		"\t\tErrorHandler: func(c *hypcontext.Context, err error) {\n" +
		"\t\t\terrors.AbortWithAppError(c, services.ErrAuthUnauthorized.With(\"reason\", err.Error()))\n" +
		"\t\t},\n" +
		"\t}))\n" +
		"\tprotected.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/me\",\n" +
		"\t\tSummary: \"Current account\",\n" +
		"\t\tTags:    []string{\"auth\"},\n" +
		"\t\tOutput:  models.MeResp{},\n" +
		"\t\tResponses: map[int]schema.ResponseSchema{\n" +
		"\t\t\t200: {Description: \"Current account\"},\n" +
		"\t\t\t401: {Description: \"Missing or invalid token\"},\n" +
		"\t\t},\n" +
		"\t}).Handle(ctrl.Me)\n\n" +
		"\t// 需 admin 角色：在 protected 之下再掛 RequireRole（角色來自 token 的 roles claim）\n" +
		"\tadmin := protected.NewGroup(\"/admin\", middleware.RequireRoleWith(middleware.RequireRoleConfig{\n" +
		"\t\tRoles: []string{services.RoleAdmin},\n" +
		"\t\tErrorHandler: func(c *hypcontext.Context, err error) {\n" +
		"\t\t\terrors.AbortWithAppError(c, services.ErrAuthForbidden.With(\"reason\", err.Error()))\n" +
		"\t\t},\n" +
		"\t}))\n" +
		"\tadmin.Schema(schema.Route{\n" +
		"\t\tMethod:  \"GET\",\n" +
		"\t\tPath:    \"/stats\",\n" +
		"\t\tSummary: \"Admin stats\",\n" +
		"\t\tTags:    []string{\"auth\", \"admin\"},\n" +
		"\t\tOutput:  models.AdminStatsResp{},\n" +
		"\t\tResponses: map[int]schema.ResponseSchema{\n" +
		"\t\t\t200: {Description: \"Stats\"},\n" +
		"\t\t\t403: {Description: \"Requires admin role\"},\n" +
		"\t\t},\n" +
		"\t}).Handle(ctrl.AdminStats)\n" +
		"}\n\n" +
		"// jwtSecret 讀取環境變數 JWT_SECRET；未設定時產生每次啟動不同的隨機金鑰並警告\n" +
		"//（既有 token 會在重啟後失效——正式環境務必設定）\n" +
		"func jwtSecret() []byte {\n" +
		"\tif s := os.Getenv(\"JWT_SECRET\"); s != \"\" {\n" +
		"\t\treturn []byte(s)\n" +
		"\t}\n" +
		"\tbuf := make([]byte, 32)\n" +
		"\tif _, err := rand.Read(buf); err != nil {\n" +
		"\t\tpanic(\"jwtSecret: crypto/rand unavailable: \" + err.Error())\n" +
		"\t}\n" +
		"\tfmt.Fprintln(os.Stderr, \"WARN: JWT_SECRET not set; using a random per-process secret (tokens won't survive restarts)\")\n" +
		"\treturn buf\n" +
		"}\n"
	return os.WriteFile(filepath.Join(projectName, "routers", "auth.go"), []byte(routes), 0644)
}

// createAPIHealth 生成健康檢查的 Schema Output DTO 與 handler
func createAPIHealth(projectName, today string) error {
	model := "// Package models 定義資料模型與 Schema 的 Request/Response DTO。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package models\n\n" +
		"// HealthResp 健康檢查回應（Schema Output）。\n" +
		"type HealthResp struct {\n" +
		"\tStatus    string `json:\"status\"`\n" +
		"\tProtocol  string `json:\"protocol\"`\n" +
		"\tTimestamp int64  `json:\"timestamp\"`\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(projectName, "app", "models", "health.go"), []byte(model), 0644); err != nil {
		return err
	}

	ctrl := "// Package controllers 承載 HTTP handler（薄控制器：解析輸入 → 呼叫 service → 寫回應）。\n" +
		"//\n" +
		"// @ai:generated by=hypgo date=" + today + "\n" +
		"package controllers\n\n" +
		"import (\n" +
		"\t\"time\"\n\n" +
		"\thypcontext \"github.com/maoxiaoyue/hypgo/pkg/context\"\n\n" +
		"\t\"" + projectName + "/app/models\"\n" +
		")\n\n" +
		"// HealthCheck 回報服務狀態與目前連線協議（HTTP/1.1、HTTP/2 或 HTTP/3）。\n" +
		"func HealthCheck(c *hypcontext.Context) {\n" +
		"\tc.JSON(200, models.HealthResp{\n" +
		"\t\tStatus:    \"ok\",\n" +
		"\t\tProtocol:  c.Protocol(),\n" +
		"\t\tTimestamp: time.Now().Unix(),\n" +
		"\t})\n" +
		"}\n"
	return os.WriteFile(filepath.Join(projectName, "app", "controllers", "health.go"), []byte(ctrl), 0644)
}

// createAPIMainFile 生成 main.go：以根 hypgo facade 一行式啟動，
// 可選初始化 hidb（config 的 database.dsn 為空時略過），路由交給 routers.Setup。
func createAPIMainFile(projectName string) error {
	content := "package main\n\n" +
		"import (\n" +
		"\t\"fmt\"\n" +
		"\t\"os\"\n\n" +
		"\t\"github.com/maoxiaoyue/hypgo\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/config\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb/mysql\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/hidb/pg\"\n" +
		"\t\"github.com/maoxiaoyue/hypgo/pkg/logger\"\n\n" +
		"\t\"" + projectName + "/routers\"\n" +
		")\n\n" +
		"// configPath 是 runtime 設定檔位置（專案根目錄 config/；.hyp/ 下的是設計時設定，兩者分離）\n" +
		"const configPath = \"config/config.yaml\"\n\n" +
		"func main() {\n" +
		"\t// 嚴格載入設定：檔案不存在或內容無效即結束，不靜默退回預設值起跑；\n" +
		"\t// logger 與 server 依 config 建立\n" +
		"\tapp, err := hypgo.NewWithConfig(configPath)\n" +
		"\tif err != nil {\n" +
		"\t\tfmt.Fprintf(os.Stderr, \"load %s: %v\\n\", configPath, err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"\tlog := app.Logger()\n\n" +
		"\t// 資料庫（database.dsn 留空則略過）；service 透過建構子注入：\n" +
		"\t//   services.NewUserService(db, log)\n" +
		"\tif db := openDatabase(app.Config(), log); db != nil {\n" +
		"\t\tdefer db.Close()\n" +
		"\t}\n\n" +
		"\t// 所有路由與中間件定義於 routers/router.go\n" +
		"\trouters.Setup(app.Server().Router())\n\n" +
		"\t// Run 會阻塞到收到 SIGINT/SIGTERM 並完成優雅關閉\n" +
		"\tif err := app.Run(); err != nil {\n" +
		"\t\tlog.Errorf(\"server error: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"}\n\n" +
		"// openDatabase 依 config 的 database 區段建立 hidb 連線；dsn 為空回傳 nil\n" +
		"func openDatabase(cfg *config.Config, log *logger.Logger) *hidb.Database {\n" +
		"\tif cfg.Database.DSN == \"\" {\n" +
		"\t\tlog.Info(\"database.dsn not set, skipping database init\")\n" +
		"\t\treturn nil\n" +
		"\t}\n\n" +
		"\tvar dialect hidb.Dialect\n" +
		"\tswitch cfg.Database.Driver {\n" +
		"\tcase \"mysql\", \"tidb\":\n" +
		"\t\tdialect = mysql.New()\n" +
		"\tdefault:\n" +
		"\t\tdialect = pg.New()\n" +
		"\t}\n\n" +
		"\tdb, err := hidb.NewWithInterface(&cfg.Database, hidb.WithDialect(dialect))\n" +
		"\tif err != nil {\n" +
		"\t\tlog.Errorf(\"database init failed: %v\", err)\n" +
		"\t\tos.Exit(1)\n" +
		"\t}\n" +
		"\treturn db\n" +
		"}\n"

	return os.WriteFile(filepath.Join(projectName, "main.go"), []byte(content), 0644)
}

type fileTemplate struct {
	Path    string
	Content string
}

func createTemplateFile(filepath, content string, data interface{}) error {
	tmpl, err := template.New("file").Parse(content)
	if err != nil {
		return err
	}

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	return tmpl.Execute(file, data)
}

func printSuccessMessage(projectName string) {
	fmt.Printf("\n✨ Successfully created HypGo API project: %s\n\n", projectName)
	fmt.Printf("📁 Project Structure:\n")
	fmt.Printf("   %s/\n", projectName)
	fmt.Printf("   ├── app/\n")
	fmt.Printf("   │   ├── controllers/    # user_controller.go, auth_controller.go, health.go（薄控制器）\n")
	fmt.Printf("   │   ├── models/         # user.go, auth.go, health.go（Bun model + Schema DTO）\n")
	fmt.Printf("   │   └── services/       # user_service.go, auth_service.go（業務邏輯 + Error Catalog）\n")
	fmt.Printf("   ├── routers/            # router.go（Setup）+ user.go / auth.go（Schema 路由）+ middleware.go\n")
	fmt.Printf("   ├── config/config.yaml  # server / database / logger\n")
	fmt.Printf("   ├── .hyp/               # llm.yaml / comment.yaml / config.yaml\n")
	fmt.Printf("   ├── migrations/         # SQL migrations\n")
	fmt.Printf("   ├── logs/               # .gitkeep（logger 輸出 logs/api.log）\n")
	fmt.Printf("   ├── certs/              # make cert → HTTP/3 用 TLS 憑證\n")
	fmt.Printf("   ├── Dockerfile / docker-compose.yml / Makefile\n")
	fmt.Printf("   └── main.go             # hypgo.NewWithConfig 嚴格載入設定 + routers.Setup\n")
	fmt.Printf("\n🚀 Quick Start:\n")
	fmt.Printf("   cd %s\n", projectName)
	fmt.Printf("   go run .                # http://localhost:8080/health\n")
	fmt.Printf("   export JWT_SECRET=...   # /api/auth/{register,login,me}（middleware.JWT + HS256）\n")
	fmt.Printf("   make cert               # 產生自簽憑證後，config.yaml 開 tls.enabled 即得 HTTP/3\n")
	fmt.Printf("   hyp generate controller order   # 新增資源 → 在 routers/router.go 註冊\n")
	fmt.Printf("   hyp lint --deep         # CI gate：Schema 完整度 + handler 型別對齊\n")
	fmt.Printf("\n")
}

// ===== File Contents =====

// configYamlContent 對齊 pkg/config.Config 的真實欄位（server / database / logger）
const configYamlContent = `# HypGo API Configuration（欄位對應 pkg/config.Config）

server:
  protocol: auto           # http1 | http2 | http3 | auto（auto = TCP 上 H1/H2，tls 啟用時同時起 H3）
  addr: :8080
  read_timeout: 30         # 秒
  write_timeout: 30
  idle_timeout: 120
  max_handlers: 1000
  max_concurrent_streams: 250
  max_read_frame_size: 1048576
  enable_graceful_restart: true
  # 可信代理網段：服務跑在 nginx / LB 之後時務必設定，否則 c.ClientIP()
  # 只回傳實際連線來源（安全預設），不會解析 X-Forwarded-For / X-Real-IP，
  # IPWhitelist / RateLimiter 等以 ClientIP 為 key 的功能會全部失準。
  # 支援 CIDR 或單一 IP。
  trusted_proxies:
    - 127.0.0.1
    - 10.0.0.0/8
  tls:
    enabled: false         # make cert 產生自簽憑證後改 true（HTTP/3 必須 TLS）
    cert_file: "certs/server.crt"
    key_file: "certs/server.key"

database:
  driver: postgres         # postgres | mysql | tidb（main.go 依此選 dialect）
  dsn: ""                  # 留空 = 不連資料庫；例：postgres://user:pass@localhost:5432/app?sslmode=disable
  max_idle_conns: 10
  max_open_conns: 100
  redis:                   # 需要時在 main.go 加上 hidb.WithRedis(redis.New())
    addr: "localhost:6379"
    password: ""
    db: 0

logger:
  level: info              # debug | info | notice | warning | emergency
  output: "logs/api.log"   # stdout 或檔案路徑
  colors: true
  rotation:
    max_size: 100MB
    max_age: 7d
    max_backups: 10
    compress: true
`

const envExampleContent = `# JWT_SECRET：HS256 簽發／驗證金鑰（routers/auth.go 讀取）。
# 未設定時每次啟動用隨機金鑰，重啟後既有 token 失效——正式環境務必設定（≥ 32 bytes）
JWT_SECRET=change-me-to-a-random-secret-of-at-least-32-bytes

# DB_DSN：供 docker-compose.yml 與 Makefile 的 migrate 目標使用；
# 應用程式本身讀 config/config.yaml 的 database.dsn（不展開環境變數）
DB_DSN=postgres://hypgo:password@localhost:5432/hypgo_db?sslmode=disable
`

const dockerfileContent = `# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git make

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

ENV TZ=Asia/Taipei

RUN addgroup -g 1000 -S appuser && \
    adduser -u 1000 -S appuser -G appuser

WORKDIR /app

COPY --from=builder /app/main .
COPY --from=builder /app/config ./config

RUN mkdir -p logs certs && \
    chown -R appuser:appuser /app

USER appuser

EXPOSE 8080 8443

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

CMD ["./main"]`

const dockerComposeContent = `version: '3.8'

services:
  api:
    build: .
    container_name: hypgo-api
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8443:8443"
    environment:
      - ENV=development
      - DB_DSN=postgres://hypgo:password@postgres:5432/hypgo_db?sslmode=disable
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=
      - JWT_SECRET=your-secret-key-change-this-in-production
    volumes:
      - ./config:/app/config:ro
      - ./logs:/app/logs
      - ./certs:/app/certs:ro
    depends_on:
      - postgres
      - redis
    networks:
      - hypgo-network

  postgres:
    image: postgres:15-alpine
    container_name: hypgo-postgres
    restart: unless-stopped
    environment:
      - POSTGRES_USER=hypgo
      - POSTGRES_PASSWORD=password
      - POSTGRES_DB=hypgo_db
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d:ro
    ports:
      - "5432:5432"
    networks:
      - hypgo-network

  redis:
    image: redis:7-alpine
    container_name: hypgo-redis
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
    ports:
      - "6379:6379"
    networks:
      - hypgo-network

volumes:
  postgres-data:
  redis-data:

networks:
  hypgo-network:
    driver: bridge`

const makefileContent = `.PHONY: help build run test clean docker migrate dev

# Variables
APP_NAME=hypgo-api
MAIN_PATH=.
DOCKER_IMAGE=$(APP_NAME):latest

# Colors
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[0;33m
NC=\033[0m

help: ## Show help
	@echo '$(GREEN)Usage:$(NC)'
	@echo '  make [target]'
	@echo ''
	@echo '$(GREEN)Targets:$(NC)'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  $(YELLOW)%-15s$(NC) %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the application
	@echo "$(GREEN)Building $(APP_NAME)...$(NC)"
	@go build -o bin/$(APP_NAME) $(MAIN_PATH)
	@echo "$(GREEN)Build complete!$(NC)"

run: ## Run the application
	@echo "$(GREEN)Running $(APP_NAME)...$(NC)"
	@go run $(MAIN_PATH)

dev: ## Run with hot reload
	@echo "$(GREEN)Running in development mode...$(NC)"
	@air

test: ## Run tests
	@echo "$(GREEN)Running tests...$(NC)"
	@go test -v -race -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Tests complete!$(NC)"

lint: ## Run linter
	@echo "$(GREEN)Running linter...$(NC)"
	@golangci-lint run ./...

fmt: ## Format code
	@echo "$(GREEN)Formatting code...$(NC)"
	@go fmt ./...
	@goimports -w .

clean: ## Clean build artifacts
	@echo "$(YELLOW)Cleaning...$(NC)"
	@rm -rf bin/ coverage.* tmp/
	@echo "$(GREEN)Clean complete!$(NC)"

docker: ## Build Docker image
	@echo "$(GREEN)Building Docker image...$(NC)"
	@docker build -t $(DOCKER_IMAGE) .

docker-run: docker ## Run Docker container
	@echo "$(GREEN)Running Docker container...$(NC)"
	@docker run -p 8080:8080 -p 8443:8443 $(DOCKER_IMAGE)

docker-compose-up: ## Start all services
	@echo "$(GREEN)Starting services...$(NC)"
	@docker-compose up -d

docker-compose-down: ## Stop all services
	@echo "$(YELLOW)Stopping services...$(NC)"
	@docker-compose down

migrate: ## Run database migrations
	@echo "$(GREEN)Running migrations...$(NC)"
	@migrate -path migrations -database "$${DB_DSN}" up

migrate-down: ## Rollback migrations
	@echo "$(YELLOW)Rolling back migrations...$(NC)"
	@migrate -path migrations -database "$${DB_DSN}" down 1

migrate-create: ## Create new migration
	@echo "$(GREEN)Creating migration: $(name)$(NC)"
	@migrate create -ext sql -dir migrations -seq $(name)

seed: ## Seed the database
	@echo "$(GREEN)Seeding database...$(NC)"
	@go run scripts/seed.go

docs: ## Generate API documentation
	@echo "$(GREEN)Generating documentation...$(NC)"
	@swag init -g main.go -o docs

install-tools: ## Install development tools
	@echo "$(GREEN)Installing development tools...$(NC)"
	@go install github.com/cosmtrek/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install github.com/swaggo/swag/cmd/swag@latest
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	@echo "$(GREEN)Tools installed!$(NC)"

cert: ## Generate self-signed certificates
	@echo "$(GREEN)Generating certificates...$(NC)"
	@mkdir -p certs
	@openssl req -x509 -newkey rsa:4096 -keyout certs/server.key -out certs/server.crt -days 365 -nodes -subj "/CN=localhost"
	@echo "$(GREEN)Certificates generated!$(NC)"

.DEFAULT_GOAL := help`

const gitignoreContent = `# Binaries
*.exe
*.exe~
*.dll
*.so
*.dylib
bin/

# Test binary
*.test

# Output
*.out
coverage.html

# Dependency directories
vendor/

# Go workspace
go.work

# Environment variables
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/
*.swp
*.swo
*~
.DS_Store

# Logs（保留 logs/.gitkeep 讓目錄存在）
logs/*
!logs/.gitkeep
*.log

# Certificates
certs/
*.pem
*.key
*.crt

# Database
*.db
*.sqlite
*.sqlite3

# Temporary files
tmp/
temp/

# Build artifacts
dist/
build/

# Air config
.air.tmp/`

const airTomlContent = `root = "."
testdata_dir = "testdata"
tmp_dir = "tmp"

[build]
  args_bin = []
  bin = "./tmp/main"
  cmd = "go build -o ./tmp/main ."
  delay = 1000
  exclude_dir = ["assets", "tmp", "vendor", "testdata", "docs", "scripts", "logs", "certs"]
  exclude_file = []
  exclude_regex = ["_test.go"]
  exclude_unchanged = false
  follow_symlink = false
  full_bin = ""
  include_dir = []
  include_ext = ["go", "tpl", "tmpl", "html"]
  include_file = []
  kill_delay = "0s"
  log = "build-errors.log"
  poll = false
  poll_interval = 0
  rerun = false
  rerun_delay = 500
  send_interrupt = false
  stop_on_error = false

[color]
  app = ""
  build = "yellow"
  main = "magenta"
  runner = "green"
  watcher = "cyan"

[log]
  main_only = false
  time = false

[misc]
  clean_on_exit = false

[screen]
  clear_on_rebuild = false
  keep_scroll = true`

const readmeContent = `# {{.ProjectName}}

API server built with the HypGo framework — Schema-first routes, HTTP/1.1 + HTTP/2 + HTTP/3 (QUIC).

## Quick Start

` + "```bash" + `
go mod tidy
go run .                 # http://localhost:8080/health
` + "```" + `

HTTP/3 needs TLS: run ` + "`make cert`" + ` (self-signed cert into ` + "`certs/`" + `), then set ` + "`server.tls.enabled: true`" + ` in ` + "`config/config.yaml`" + `.
` + "`server.protocol: auto`" + ` serves HTTP/1.1 + HTTP/2 over TCP and, once TLS is on, HTTP/3 over UDP as well.

## Endpoints

| Method | Path | Handler |
|--------|------|---------|
| GET | ` + "`/health`" + ` | ` + "`controllers.HealthCheck`" + ` |
| POST | ` + "`/api/auth/register`" + ` | ` + "`AuthController.Register`" + ` (bcrypt, 201 / 409 / 422) |
| POST | ` + "`/api/auth/login`" + ` | ` + "`AuthController.Login`" + ` → ` + "`{token, expires_at}`" + ` (HS256) |
| GET | ` + "`/api/auth/me`" + ` | ` + "`AuthController.Me`" + ` (requires ` + "`Authorization: Bearer <token>`" + `) |
| GET | ` + "`/api/auth/admin/stats`" + ` | ` + "`AuthController.AdminStats`" + ` (token + ` + "`admin`" + ` role; 403 otherwise) |
| GET | ` + "`/api/user`" + ` | ` + "`UserController.List`" + ` |
| POST | ` + "`/api/user`" + ` | ` + "`UserController.Create`" + ` (` + "`c.BindInput`" + ` → validate → 201) |
| GET | ` + "`/api/user/:id`" + ` | ` + "`UserController.Get`" + ` |
| PUT | ` + "`/api/user/:id`" + ` | ` + "`UserController.Update`" + ` |
| DELETE | ` + "`/api/user/:id`" + ` | ` + "`UserController.Delete`" + ` |

Every route is registered with ` + "`r.Schema(...)`" + ` (Input/Output types), so ` + "`hyp lint --deep`" + `,
` + "`contract.TestAll`" + ` and ` + "`.hyp/context.yaml`" + ` all understand the API without extra work.

## Authentication

Uses the framework's ` + "`middleware.JWT`" + ` with the built-in HS256 signer/validator (no external JWT library):

` + "```bash" + `
export JWT_SECRET=$(openssl rand -hex 32)   # see .env.example; random per-process secret if unset
curl -X POST localhost:8080/api/auth/register -H 'Content-Type: application/json' -d '{"email":"a@b.c","password":"secret123"}'
TOKEN=$(curl -s -X POST localhost:8080/api/auth/login -H 'Content-Type: application/json' -d '{"email":"a@b.c","password":"secret123"}' | jq -r .token)
curl localhost:8080/api/auth/me -H "Authorization: Bearer $TOKEN"
` + "```" + `

- ` + "`routers/auth.go`" + `: public register/login + a ` + "`protected`" + ` group (` + "`r.NewGroup(\"/api/auth\", middleware.JWT(...))`" + `). Put any route that needs a login on that group.
- ` + "`services/auth_service.go`" + `: bcrypt password hashing, ` + "`middleware.SignHS256`" + ` for tokens. Accounts live in ` + "`MemoryAccountStore`" + ` (in-process, cleared on restart) — implement ` + "`AccountStore`" + ` on top of ` + "`hidb`" + ` and pass it to ` + "`NewAuthService`" + ` for persistence.
- Claims in handlers: ` + "`middleware.JWTClaimsFrom(c, \"\")`" + ` → ` + "`*middleware.JWTClaims`" + ` (` + "`Subject`" + ` = account ID, ` + "`Roles`" + `).
- Roles: the **first registered account becomes ` + "`admin`" + `** (bootstrap); roles travel in the token's ` + "`roles`" + ` claim and the JWT middleware exposes them via ` + "`c.GetRoles()`" + `. Gate a group with ` + "`middleware.RequireRole(\"admin\")`" + ` (see the ` + "`admin`" + ` group in ` + "`routers/auth.go`" + `).

## Adding a resource

` + "```bash" + `
hyp generate controller order   # app/controllers/order_controller.go + routers/order.go
hyp generate model order        # app/models/order.go (Bun model + Req/Resp DTOs)
hyp generate service order      # app/services/order_service.go
` + "```" + `

Then add ` + "`RegisterOrderRoutes(r)`" + ` to ` + "`routers/router.go`" + `.

## Database

Set ` + "`database.dsn`" + ` in ` + "`config/config.yaml`" + ` (` + "`driver`" + `: postgres / mysql / tidb). ` + "`main.go`" + ` opens
` + "`hidb`" + ` with the matching dialect and skips it when ` + "`dsn`" + ` is empty. Inject it into services:

` + "```go" + `
svc := services.NewUserService(db, log)
` + "```" + `

Redis: add ` + "`hidb.WithRedis(redis.New())`" + ` (import ` + "`github.com/maoxiaoyue/hypgo/pkg/hidb/redis`" + `) next to ` + "`WithDialect`" + `.

## Project Structure

` + "```" + `
.
├── app/
│   ├── controllers/    # thin handlers (parse → service → respond)
│   ├── models/         # Bun models + Schema Req/Resp DTOs
│   └── services/       # business logic + Error Catalog
├── routers/            # router.go (Setup) + <resource>.go Schema routes + middleware.go
├── config/config.yaml  # server / database / logger
├── .hyp/               # llm.yaml / comment.yaml / config.yaml (AI tooling)
├── migrations/         # SQL migrations (make migrate)
├── logs/               # logger output (logs/api.log)
├── certs/              # make cert
└── main.go             # hypgo.NewWithConfig (strict config) + routers.Setup
` + "```" + `

## Tooling

` + "```bash" + `
make dev          # hot reload (air)
make test         # go test -race
make cert         # self-signed TLS cert for HTTP/3
make migrate      # golang-migrate (needs DB_DSN, see .env.example)
hyp lint --deep   # Schema completeness + handler type alignment (CI gate)
hyp context       # regenerate .hyp/context.yaml for AI tools
` + "```" + ``

const goModContent = `module {{.ProjectName}}

go 1.24

require (
	github.com/maoxiaoyue/hypgo v0.9.1
	golang.org/x/crypto v0.47.0 // bcrypt（auth_service.go）
)
`

const createUsersUpSQL = `
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    avatar VARCHAR(500),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);

-- 更新時間觸發器
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_users_updated_at BEFORE UPDATE
    ON users FOR EACH ROW EXECUTE PROCEDURE update_updated_at_column();
`

const createUsersDownSQL = `DROP TRIGGER IF EXISTS update_users_updated_at ON users;
DROP FUNCTION IF EXISTS update_updated_at_column();
DROP TABLE IF EXISTS users;`

const createRolesUpSQL = `CREATE TABLE IF NOT EXISTS roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS permissions (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) UNIQUE NOT NULL,
    resource VARCHAR(100),
    action VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,
    role_id INTEGER REFERENCES roles(id) ON DELETE CASCADE,
    assigned_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id INTEGER REFERENCES roles(id) ON DELETE CASCADE,
    permission_id INTEGER REFERENCES permissions(id) ON DELETE CASCADE,
    granted_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (role_id, permission_id)
);

-- 插入默認角色
INSERT INTO roles (name, description) VALUES 
    ('admin', 'Administrator with full access'),
    ('user', 'Regular user with limited access'),
    ('moderator', 'Moderator with content management access')
ON CONFLICT (name) DO NOTHING;

-- 插入默認權限
INSERT INTO permissions (name, resource, action) VALUES 
    ('users.read', 'users', 'read'),
    ('users.write', 'users', 'write'),
    ('users.delete', 'users', 'delete'),
    ('posts.read', 'posts', 'read'),
    ('posts.write', 'posts', 'write'),
    ('posts.delete', 'posts', 'delete')
ON CONFLICT (name) DO NOTHING;

-- 為 admin 角色分配所有權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;

-- 為 user 角色分配讀取權限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'user' AND p.action = 'read'
ON CONFLICT DO NOTHING;`

const createRolesDownSQL = `DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;`
