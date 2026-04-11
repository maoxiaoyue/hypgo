package config

import (
	"fmt"
	"os"
	"strings"
)

// LLMConfig 控制 Manifest 智慧增強的 LLM 層
type LLMConfig struct {
	// Mode 控制 LLM 增強模式
	// "none"（預設）：不使用 LLM，只做純 Go 推斷（零成本）
	// "rag"：使用 RAG（向量資料庫 + embedding + LLM 生成）
	// "ollama"：直接連接本地 Ollama 伺服器
	// "api"：連接遠端 API（OpenAI、Anthropic、Gemini 等）
	Mode string `mapstructure:"mode" yaml:"mode"`

	// RAG 模式配置（mode=rag 時使用）
	RAG RAGConfig `mapstructure:"rag" yaml:"rag"`

	// Ollama 模式配置（mode=ollama 時使用）
	Ollama OllamaConfig `mapstructure:"ollama" yaml:"ollama"`

	// API 模式配置（mode=api 時使用）
	API APIConfig `mapstructure:"api" yaml:"api"`
}

// RAGConfig RAG（Retrieval-Augmented Generation）模式配置
type RAGConfig struct {
	// Embedding 設定
	EmbeddingModel string `mapstructure:"embedding_model" yaml:"embedding_model"` // embedding 模型名 e.g. "nomic-embed-text"
	EmbeddingURL   string `mapstructure:"embedding_url" yaml:"embedding_url"`     // embedding API 端點（預設用 Ollama）

	// 向量資料庫設定
	VectorStore    string `mapstructure:"vector_store" yaml:"vector_store"`         // "chroma", "qdrant", "milvus", "faiss"
	VectorStoreURL string `mapstructure:"vector_store_url" yaml:"vector_store_url"` // 向量資料庫連線 URL
	Collection     string `mapstructure:"collection" yaml:"collection"`             // 集合名稱
	TopK           int    `mapstructure:"top_k" yaml:"top_k"`                       // 檢索結果數量（預設 5）

	// 生成用 LLM（可選，用於根據檢索結果生成回答）
	GeneratorModel string `mapstructure:"generator_model" yaml:"generator_model"` // 生成模型 e.g. "llama3"
	GeneratorURL   string `mapstructure:"generator_url" yaml:"generator_url"`     // 生成 API 端點（預設 Ollama localhost）
}

// OllamaConfig 本地 Ollama 模式配置
type OllamaConfig struct {
	URL       string `mapstructure:"url" yaml:"url"`             // Ollama API URL（預設 http://localhost:11434）
	Model     string `mapstructure:"model" yaml:"model"`         // 模型名稱 e.g. "llama3", "codellama", "mistral"
	Timeout   int    `mapstructure:"timeout" yaml:"timeout"`     // 請求逾時秒數（預設 30）
	MaxTokens int    `mapstructure:"max_tokens" yaml:"max_tokens"` // 最大回應 token 數（預設 256）
}

// APIConfig 遠端 API 模式配置
type APIConfig struct {
	Provider  string `mapstructure:"provider" yaml:"provider"`     // "openai", "anthropic", "gemini", "custom"
	BaseURL   string `mapstructure:"base_url" yaml:"base_url"`     // API 端點（custom 時必填；其他 provider 有預設值）
	APIKey    string `mapstructure:"api_key" yaml:"api_key"`       // API 金鑰（支援 ${ENV_VAR} 環境變數展開）
	Model     string `mapstructure:"model" yaml:"model"`           // 模型名稱 e.g. "gpt-4o-mini", "claude-haiku-4-5"
	Timeout   int    `mapstructure:"timeout" yaml:"timeout"`       // 請求逾時秒數（預設 30）
	MaxTokens int    `mapstructure:"max_tokens" yaml:"max_tokens"` // 最大回應 token（預設 256）
}

// ApplyDefaults 為 LLMConfig 填入預設值
func (c *LLMConfig) ApplyDefaults() {
	if c.Mode == "" {
		c.Mode = "none"
	}

	// RAG 預設值
	if c.RAG.TopK == 0 {
		c.RAG.TopK = 5
	}
	if c.RAG.EmbeddingURL == "" {
		c.RAG.EmbeddingURL = "http://localhost:11434"
	}
	if c.RAG.GeneratorURL == "" {
		c.RAG.GeneratorURL = "http://localhost:11434"
	}

	// Ollama 預設值
	if c.Ollama.URL == "" {
		c.Ollama.URL = "http://localhost:11434"
	}
	if c.Ollama.Timeout == 0 {
		c.Ollama.Timeout = 30
	}
	if c.Ollama.MaxTokens == 0 {
		c.Ollama.MaxTokens = 256
	}

	// API 預設值
	if c.API.Timeout == 0 {
		c.API.Timeout = 30
	}
	if c.API.MaxTokens == 0 {
		c.API.MaxTokens = 256
	}
	// Provider 預設 BaseURL
	if c.API.BaseURL == "" {
		switch c.API.Provider {
		case "openai":
			c.API.BaseURL = "https://api.openai.com/v1"
		case "anthropic":
			c.API.BaseURL = "https://api.anthropic.com/v1"
		case "gemini":
			c.API.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
	}
}

// Validate 驗證 LLMConfig 的邏輯正確性
func (c *LLMConfig) Validate() error {
	switch c.Mode {
	case "none":
		return nil

	case "ollama":
		if c.Ollama.Model == "" {
			return fmt.Errorf("llm.ollama.model is required when mode=ollama")
		}

	case "api":
		if c.API.Provider == "" {
			return fmt.Errorf("llm.api.provider is required when mode=api")
		}
		switch c.API.Provider {
		case "openai", "anthropic", "gemini", "custom":
		default:
			return fmt.Errorf("llm.api.provider must be one of: openai, anthropic, gemini, custom; got %q", c.API.Provider)
		}
		if c.API.Provider == "custom" && c.API.BaseURL == "" {
			return fmt.Errorf("llm.api.base_url is required when provider=custom")
		}
		if c.API.Model == "" {
			return fmt.Errorf("llm.api.model is required when mode=api")
		}
		// API key 可透過環境變數設定，所以允許為空（但展開後不可為空）
		resolved := c.API.ResolvedAPIKey()
		if resolved == "" {
			return fmt.Errorf("llm.api.api_key is required when mode=api (supports ${ENV_VAR} syntax)")
		}

	case "rag":
		if c.RAG.EmbeddingModel == "" {
			return fmt.Errorf("llm.rag.embedding_model is required when mode=rag")
		}
		if c.RAG.VectorStore == "" {
			return fmt.Errorf("llm.rag.vector_store is required when mode=rag")
		}
		switch c.RAG.VectorStore {
		case "chroma", "qdrant", "milvus", "faiss":
		default:
			return fmt.Errorf("llm.rag.vector_store must be one of: chroma, qdrant, milvus, faiss; got %q", c.RAG.VectorStore)
		}
		if c.RAG.VectorStoreURL == "" {
			return fmt.Errorf("llm.rag.vector_store_url is required when mode=rag")
		}
		if c.RAG.GeneratorModel == "" {
			return fmt.Errorf("llm.rag.generator_model is required when mode=rag")
		}

	default:
		return fmt.Errorf("llm.mode must be one of: none, rag, ollama, api; got %q", c.Mode)
	}

	return nil
}

// ResolvedAPIKey 展開 API key 中的環境變數引用
// 支援 ${ENV_VAR} 語法
func (c *APIConfig) ResolvedAPIKey() string {
	return expandEnvVars(c.APIKey)
}

// expandEnvVars 展開字串中的 ${ENV_VAR} 引用
func expandEnvVars(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return os.Expand(s, os.Getenv)
}

// IsEnabled 回傳 LLM 增強是否啟用
func (c *LLMConfig) IsEnabled() bool {
	return c.Mode != "" && c.Mode != "none"
}

// LoadLLMConfig 從指定路徑載入 LLM 配置
// 若檔案不存在，回傳預設配置（mode=none）
func LoadLLMConfig(path string) (*LLMConfig, error) {
	cfg := &LLMConfig{}

	if path == "" {
		cfg.ApplyDefaults()
		return cfg, nil
	}

	// 嘗試讀取檔案
	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg.ApplyDefaults()
		return cfg, nil
	}

	if err := LoadYAML(path, cfg); err != nil {
		return nil, fmt.Errorf("failed to load LLM config from %s: %w", path, err)
	}

	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("LLM config validation failed: %w", err)
	}

	return cfg, nil
}
