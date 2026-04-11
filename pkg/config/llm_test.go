package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLLMConfigDefaults(t *testing.T) {
	cfg := &LLMConfig{}
	cfg.ApplyDefaults()

	if cfg.Mode != "none" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "none")
	}
	if cfg.Ollama.URL != "http://localhost:11434" {
		t.Errorf("Ollama.URL = %q, want %q", cfg.Ollama.URL, "http://localhost:11434")
	}
	if cfg.Ollama.Timeout != 30 {
		t.Errorf("Ollama.Timeout = %d, want 30", cfg.Ollama.Timeout)
	}
	if cfg.Ollama.MaxTokens != 256 {
		t.Errorf("Ollama.MaxTokens = %d, want 256", cfg.Ollama.MaxTokens)
	}
	if cfg.API.Timeout != 30 {
		t.Errorf("API.Timeout = %d, want 30", cfg.API.Timeout)
	}
	if cfg.RAG.TopK != 5 {
		t.Errorf("RAG.TopK = %d, want 5", cfg.RAG.TopK)
	}
}

func TestLLMConfigValidateNone(t *testing.T) {
	cfg := &LLMConfig{Mode: "none"}
	if err := cfg.Validate(); err != nil {
		t.Errorf("mode=none should pass validation, got: %v", err)
	}
}

func TestLLMConfigValidateInvalidMode(t *testing.T) {
	cfg := &LLMConfig{Mode: "invalid"}
	if err := cfg.Validate(); err == nil {
		t.Error("invalid mode should fail validation")
	}
}

func TestLLMConfigValidateOllama(t *testing.T) {
	// 缺少 model
	cfg := &LLMConfig{Mode: "ollama"}
	if err := cfg.Validate(); err == nil {
		t.Error("ollama without model should fail")
	}

	// 正確配置
	cfg.Ollama.Model = "llama3"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid ollama config should pass, got: %v", err)
	}
}

func TestLLMConfigValidateAPI(t *testing.T) {
	// 缺少 provider
	cfg := &LLMConfig{Mode: "api"}
	if err := cfg.Validate(); err == nil {
		t.Error("api without provider should fail")
	}

	// 缺少 model
	cfg.API.Provider = "openai"
	if err := cfg.Validate(); err == nil {
		t.Error("api without model should fail")
	}

	// 缺少 api_key
	cfg.API.Model = "gpt-4o-mini"
	if err := cfg.Validate(); err == nil {
		t.Error("api without api_key should fail")
	}

	// 使用環境變數
	os.Setenv("TEST_API_KEY", "sk-test-123")
	defer os.Unsetenv("TEST_API_KEY")
	cfg.API.APIKey = "${TEST_API_KEY}"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid api config should pass, got: %v", err)
	}

	// 無效 provider
	cfg.API.Provider = "unknown"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid provider should fail")
	}

	// custom 需要 base_url
	cfg.API.Provider = "custom"
	cfg.API.BaseURL = ""
	if err := cfg.Validate(); err == nil {
		t.Error("custom provider without base_url should fail")
	}
}

func TestLLMConfigValidateRAG(t *testing.T) {
	cfg := &LLMConfig{Mode: "rag"}

	// 缺少 embedding_model
	if err := cfg.Validate(); err == nil {
		t.Error("rag without embedding_model should fail")
	}

	cfg.RAG.EmbeddingModel = "nomic-embed-text"
	// 缺少 vector_store
	if err := cfg.Validate(); err == nil {
		t.Error("rag without vector_store should fail")
	}

	cfg.RAG.VectorStore = "chroma"
	// 缺少 vector_store_url
	if err := cfg.Validate(); err == nil {
		t.Error("rag without vector_store_url should fail")
	}

	cfg.RAG.VectorStoreURL = "http://localhost:8000"
	// 缺少 generator_model
	if err := cfg.Validate(); err == nil {
		t.Error("rag without generator_model should fail")
	}

	cfg.RAG.GeneratorModel = "llama3"
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid rag config should pass, got: %v", err)
	}

	// 無效 vector_store
	cfg.RAG.VectorStore = "unknown"
	if err := cfg.Validate(); err == nil {
		t.Error("invalid vector_store should fail")
	}
}

func TestLLMConfigAPIBaseURLDefaults(t *testing.T) {
	tests := []struct {
		provider string
		wantURL  string
	}{
		{"openai", "https://api.openai.com/v1"},
		{"anthropic", "https://api.anthropic.com/v1"},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta"},
		{"custom", ""}, // custom 不設預設
	}

	for _, tt := range tests {
		cfg := &LLMConfig{
			API: APIConfig{Provider: tt.provider},
		}
		cfg.ApplyDefaults()
		if cfg.API.BaseURL != tt.wantURL {
			t.Errorf("provider=%s: BaseURL = %q, want %q", tt.provider, cfg.API.BaseURL, tt.wantURL)
		}
	}
}

func TestResolvedAPIKey(t *testing.T) {
	// 直接值
	api := &APIConfig{APIKey: "sk-direct-key"}
	if got := api.ResolvedAPIKey(); got != "sk-direct-key" {
		t.Errorf("direct key = %q, want %q", got, "sk-direct-key")
	}

	// 環境變數
	os.Setenv("MY_SECRET_KEY", "sk-from-env")
	defer os.Unsetenv("MY_SECRET_KEY")
	api.APIKey = "${MY_SECRET_KEY}"
	if got := api.ResolvedAPIKey(); got != "sk-from-env" {
		t.Errorf("env key = %q, want %q", got, "sk-from-env")
	}

	// 不存在的環境變數
	api.APIKey = "${NONEXISTENT_KEY}"
	if got := api.ResolvedAPIKey(); got != "" {
		t.Errorf("missing env key = %q, want empty", got)
	}
}

func TestLLMConfigIsEnabled(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"", false},
		{"none", false},
		{"ollama", true},
		{"api", true},
		{"rag", true},
	}
	for _, tt := range tests {
		cfg := &LLMConfig{Mode: tt.mode}
		if got := cfg.IsEnabled(); got != tt.want {
			t.Errorf("mode=%q: IsEnabled() = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

func TestLoadLLMConfigEmpty(t *testing.T) {
	cfg, err := LoadLLMConfig("")
	if err != nil {
		t.Fatalf("empty path should succeed, got: %v", err)
	}
	if cfg.Mode != "none" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "none")
	}
}

func TestLoadLLMConfigNotExist(t *testing.T) {
	cfg, err := LoadLLMConfig("/nonexistent/llm.yaml")
	if err != nil {
		t.Fatalf("missing file should return defaults, got: %v", err)
	}
	if cfg.Mode != "none" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "none")
	}
}

func TestLoadLLMConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm.yaml")

	content := `mode: ollama
ollama:
  model: llama3
  url: http://localhost:11434
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadLLMConfig(path)
	if err != nil {
		t.Fatalf("LoadLLMConfig failed: %v", err)
	}
	if cfg.Mode != "ollama" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "ollama")
	}
	if cfg.Ollama.Model != "llama3" {
		t.Errorf("Ollama.Model = %q, want %q", cfg.Ollama.Model, "llama3")
	}
}

func TestLoadLLMConfigInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "llm.yaml")

	// mode=api 但缺少必要欄位
	content := `mode: api
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLLMConfig(path)
	if err == nil {
		t.Error("invalid config should fail validation")
	}
}
