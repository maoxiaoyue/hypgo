package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// NewLLMEnricherFromConfig 根據 LLMConfig 建立對應的 LLMEnricher 實作
// mode=none 時回傳 nil（不使用 LLM）
func NewLLMEnricherFromConfig(cfg *config.LLMConfig) (LLMEnricher, error) {
	if cfg == nil || !cfg.IsEnabled() {
		return nil, nil
	}

	switch cfg.Mode {
	case "ollama":
		return newOllamaEnricher(&cfg.Ollama)
	case "api":
		return newAPIEnricher(&cfg.API)
	case "rag":
		return newRAGEnricher(&cfg.RAG)
	default:
		return nil, fmt.Errorf("unsupported LLM mode: %s", cfg.Mode)
	}
}

// --- Prompt Builder ---

// buildEnrichPrompt 將路由資訊組成給 LLM 的 prompt
func buildEnrichPrompt(route RouteManifest) string {
	var sb strings.Builder
	sb.WriteString("You are a technical API documentation writer. ")
	sb.WriteString("Based on the following route information, generate a concise summary and description. ")
	sb.WriteString("Respond in JSON format: {\"summary\": \"...\", \"description\": \"...\"}\n\n")

	sb.WriteString("Route:\n")

	proto := route.Protocol
	if proto == "" {
		proto = "rest"
	}
	sb.WriteString(fmt.Sprintf("  Protocol: %s\n", proto))

	if route.Method != "" {
		sb.WriteString(fmt.Sprintf("  Method: %s\n", route.Method))
	}
	if route.Path != "" {
		sb.WriteString(fmt.Sprintf("  Path: %s\n", route.Path))
	}
	if route.Command != "" {
		sb.WriteString(fmt.Sprintf("  Command: %s\n", route.Command))
	}
	if route.InputType != "" {
		sb.WriteString(fmt.Sprintf("  InputType: %s\n", route.InputType))
	}
	if route.OutputType != "" {
		sb.WriteString(fmt.Sprintf("  OutputType: %s\n", route.OutputType))
	}
	if len(route.HandlerNames) > 0 {
		sb.WriteString(fmt.Sprintf("  Handler: %s\n", route.HandlerNames[0]))
	}
	if len(route.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(route.Tags, ", ")))
	}
	if route.Summary != "" {
		sb.WriteString(fmt.Sprintf("  CurrentSummary: %s\n", route.Summary))
	}

	return sb.String()
}

// llmResponse 是 LLM 回覆的 JSON 結構
type llmResponse struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
}

// parseEnrichResponse 解析 LLM 回覆並回填到 RouteManifest
func parseEnrichResponse(response string, route RouteManifest) RouteManifest {
	// 嘗試從回覆中提取 JSON
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return route
	}

	var resp llmResponse
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return route
	}

	// 只在空白時回填
	if route.Summary == "" && resp.Summary != "" {
		route.Summary = sanitizeResponse(resp.Summary)
	}
	if route.Description == "" && resp.Description != "" {
		route.Description = sanitizeResponse(resp.Description)
	}

	return route
}

// extractJSON 從 LLM 回覆中提取 JSON 物件
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(s, "}")
	if end < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}

// sanitizeResponse 清理 LLM 回覆，防止注入
func sanitizeResponse(s string) string {
	s = strings.TrimSpace(s)
	// 移除潛在的 HTML/script 標籤
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	// 限制長度
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}

// ============================================================
// OllamaEnricher — 連接本地 Ollama
// ============================================================

type OllamaEnricher struct {
	url       string
	model     string
	timeout   time.Duration
	maxTokens int
	client    *http.Client
}

func newOllamaEnricher(cfg *config.OllamaConfig) (*OllamaEnricher, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("ollama model is required")
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &OllamaEnricher{
		url:       cfg.URL,
		model:     cfg.Model,
		timeout:   timeout,
		maxTokens: cfg.MaxTokens,
		client:    &http.Client{Timeout: timeout},
	}, nil
}

// ollamaRequest Ollama /api/generate 請求
type ollamaRequest struct {
	Model  string         `json:"model"`
	Prompt string         `json:"prompt"`
	Stream bool           `json:"stream"`
	Options map[string]int `json:"options,omitempty"`
}

// ollamaResponse Ollama /api/generate 回應
type ollamaResponse struct {
	Response string `json:"response"`
}

func (e *OllamaEnricher) EnrichRoute(route RouteManifest) RouteManifest {
	prompt := buildEnrichPrompt(route)

	reqBody := ollamaRequest{
		Model:  e.model,
		Prompt: prompt,
		Stream: false,
	}
	if e.maxTokens > 0 {
		reqBody.Options = map[string]int{"num_predict": e.maxTokens}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return route
	}

	resp, err := e.client.Post(e.url+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return route
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return route
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return route
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return route
	}

	return parseEnrichResponse(ollamaResp.Response, route)
}

// ============================================================
// APIEnricher — 連接遠端 API（OpenAI / Anthropic / Gemini / Custom）
// ============================================================

type APIEnricher struct {
	provider  string
	baseURL   string
	apiKey    string
	model     string
	timeout   time.Duration
	maxTokens int
	client    *http.Client
}

func newAPIEnricher(cfg *config.APIConfig) (*APIEnricher, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("api model is required")
	}
	apiKey := cfg.ResolvedAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &APIEnricher{
		provider:  cfg.Provider,
		baseURL:   cfg.BaseURL,
		apiKey:    apiKey,
		model:     cfg.Model,
		timeout:   timeout,
		maxTokens: cfg.MaxTokens,
		client:    &http.Client{Timeout: timeout},
	}, nil
}

func (e *APIEnricher) EnrichRoute(route RouteManifest) RouteManifest {
	prompt := buildEnrichPrompt(route)

	switch e.provider {
	case "anthropic":
		return e.enrichAnthropic(prompt, route)
	default:
		// OpenAI / Gemini / Custom 都用 OpenAI-compatible format
		return e.enrichOpenAI(prompt, route)
	}
}

// openAIChatRequest OpenAI chat completion 請求
type openAIChatRequest struct {
	Model       string            `json:"model"`
	Messages    []openAIMessage   `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature float64           `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (e *APIEnricher) enrichOpenAI(prompt string, route RouteManifest) RouteManifest {
	reqBody := openAIChatRequest{
		Model: e.model,
		Messages: []openAIMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   e.maxTokens,
		Temperature: 0.3,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return route
	}

	url := e.baseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return route
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return route
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return route
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return route
	}

	var chatResp openAIChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return route
	}

	if len(chatResp.Choices) == 0 {
		return route
	}

	return parseEnrichResponse(chatResp.Choices[0].Message.Content, route)
}

// anthropicRequest Anthropic messages 請求
type anthropicRequest struct {
	Model     string            `json:"model"`
	Messages  []anthropicMsg    `json:"messages"`
	MaxTokens int               `json:"max_tokens"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (e *APIEnricher) enrichAnthropic(prompt string, route RouteManifest) RouteManifest {
	reqBody := anthropicRequest{
		Model: e.model,
		Messages: []anthropicMsg{
			{Role: "user", Content: prompt},
		},
		MaxTokens: e.maxTokens,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return route
	}

	url := e.baseURL + "/messages"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return route
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.client.Do(req)
	if err != nil {
		return route
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return route
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return route
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return route
	}

	if len(anthropicResp.Content) == 0 {
		return route
	}

	return parseEnrichResponse(anthropicResp.Content[0].Text, route)
}

// ============================================================
// RAGEnricher — RAG 模式（Embedding + 向量檢索 + LLM 生成）
// ============================================================

type RAGEnricher struct {
	embeddingModel string
	embeddingURL   string
	vectorStore    string
	vectorStoreURL string
	collection     string
	topK           int
	generatorModel string
	generatorURL   string
	client         *http.Client
}

func newRAGEnricher(cfg *config.RAGConfig) (*RAGEnricher, error) {
	if cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("rag embedding_model is required")
	}
	if cfg.VectorStore == "" {
		return nil, fmt.Errorf("rag vector_store is required")
	}
	return &RAGEnricher{
		embeddingModel: cfg.EmbeddingModel,
		embeddingURL:   cfg.EmbeddingURL,
		vectorStore:    cfg.VectorStore,
		vectorStoreURL: cfg.VectorStoreURL,
		collection:     cfg.Collection,
		topK:           cfg.TopK,
		generatorModel: cfg.GeneratorModel,
		generatorURL:   cfg.GeneratorURL,
		client:         &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (e *RAGEnricher) EnrichRoute(route RouteManifest) RouteManifest {
	// Step 1: 組建查詢文本
	queryText := buildQueryText(route)

	// Step 2: 取得 embedding
	embedding, err := e.getEmbedding(queryText)
	if err != nil || len(embedding) == 0 {
		return route
	}

	// Step 3: 向量檢索
	context, err := e.queryVectorStore(embedding)
	if err != nil || context == "" {
		return route
	}

	// Step 4: 用 LLM 生成（帶檢索的上下文）
	prompt := buildRAGPrompt(route, context)
	response, err := e.generate(prompt)
	if err != nil || response == "" {
		return route
	}

	return parseEnrichResponse(response, route)
}

func buildQueryText(route RouteManifest) string {
	parts := []string{}
	if route.Method != "" {
		parts = append(parts, route.Method)
	}
	if route.Path != "" {
		parts = append(parts, route.Path)
	}
	if route.Command != "" {
		parts = append(parts, route.Command)
	}
	if route.InputType != "" {
		parts = append(parts, "input:"+route.InputType)
	}
	if route.OutputType != "" {
		parts = append(parts, "output:"+route.OutputType)
	}
	return strings.Join(parts, " ")
}

func buildRAGPrompt(route RouteManifest, context string) string {
	var sb strings.Builder
	sb.WriteString("Based on the following code context and route information, ")
	sb.WriteString("generate a concise summary and description for this API endpoint. ")
	sb.WriteString("Respond in JSON format: {\"summary\": \"...\", \"description\": \"...\"}\n\n")

	sb.WriteString("Code Context:\n")
	sb.WriteString(context)
	sb.WriteString("\n\n")

	sb.WriteString("Route:\n")
	sb.WriteString(buildEnrichPrompt(route))

	return sb.String()
}

// ollamaEmbeddingRequest Ollama embedding 請求
type ollamaEmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
}

func (e *RAGEnricher) getEmbedding(text string) ([]float64, error) {
	reqBody := ollamaEmbeddingRequest{
		Model:  e.embeddingModel,
		Prompt: text,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := e.client.Post(e.embeddingURL+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API returned %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var embResp ollamaEmbeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, err
	}

	return embResp.Embedding, nil
}

// chromaQueryRequest Chroma 向量查詢請求
type chromaQueryRequest struct {
	QueryEmbeddings [][]float64 `json:"query_embeddings"`
	NResults        int         `json:"n_results"`
}

type chromaQueryResponse struct {
	Documents [][]string `json:"documents"`
}

func (e *RAGEnricher) queryVectorStore(embedding []float64) (string, error) {
	switch e.vectorStore {
	case "chroma":
		return e.queryChroma(embedding)
	default:
		// 其他向量資料庫的實作可在此擴展
		return "", fmt.Errorf("vector store %q not yet implemented", e.vectorStore)
	}
}

func (e *RAGEnricher) queryChroma(embedding []float64) (string, error) {
	reqBody := chromaQueryRequest{
		QueryEmbeddings: [][]float64{embedding},
		NResults:        e.topK,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v1/collections/%s/query", e.vectorStoreURL, e.collection)
	resp, err := e.client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("chroma query returned %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var chromaResp chromaQueryResponse
	if err := json.Unmarshal(respBody, &chromaResp); err != nil {
		return "", err
	}

	// 合併檢索到的文檔
	var docs []string
	for _, docSet := range chromaResp.Documents {
		docs = append(docs, docSet...)
	}

	if len(docs) == 0 {
		return "", nil
	}

	return strings.Join(docs, "\n---\n"), nil
}

func (e *RAGEnricher) generate(prompt string) (string, error) {
	reqBody := ollamaRequest{
		Model:  e.generatorModel,
		Prompt: prompt,
		Stream: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := e.client.Post(e.generatorURL+"/api/generate", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("generator returned %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return "", err
	}

	return ollamaResp.Response, nil
}
