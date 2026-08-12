// llm.go — LLM 評判器共用的最小 HTTP 客戶端
//
// 支援與 pkg/manifest 的 LLM enricher 相同的三種 wire format
//（ollama /api/generate、OpenAI 相容 /chat/completions、Anthropic /messages），
// 設定沿用 pkg/config 的 LLMConfig（.hyp/llm.yaml）。刻意不 import
// pkg/manifest：那是 server 會載入的重量級套件，eval 只需要百餘行 HTTP 呼叫
package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
)

// httpClientFor 依 LLMConfig 的 timeout 建立 client（呼叫端未注入時使用）
func httpClientFor(cfg *config.LLMConfig) *http.Client {
	timeout := 30
	switch cfg.Mode {
	case config.LLMModeOllama:
		if cfg.Ollama.Timeout > 0 {
			timeout = cfg.Ollama.Timeout
		}
	case config.LLMModeAPI:
		if cfg.API.Timeout > 0 {
			timeout = cfg.API.Timeout
		}
	}
	return &http.Client{Timeout: time.Duration(timeout) * time.Second}
}

// completeText 送出 prompt 並回傳模型的文字回應
func completeText(ctx context.Context, cfg *config.LLMConfig, client *http.Client, prompt string) (string, error) {
	if cfg == nil || !cfg.IsEnabled() {
		return "", fmt.Errorf("eval: LLM config is nil or mode=none (set .hyp/llm.yaml or the Config field)")
	}
	if client == nil {
		client = httpClientFor(cfg)
	}

	switch cfg.Mode {
	case config.LLMModeOllama:
		return ollamaGenerate(ctx, cfg, client, prompt)
	case config.LLMModeAPI:
		if cfg.API.Provider == "anthropic" {
			return anthropicComplete(ctx, cfg, client, prompt)
		}
		// openai / gemini(openai 相容端點) / custom 一律走 chat/completions
		return openAIComplete(ctx, cfg, client, prompt)
	default:
		return "", fmt.Errorf("eval: unsupported llm mode %q", cfg.Mode)
	}
}

// ollamaGenerate 呼叫 Ollama /api/generate（format=json 強制 JSON 輸出）
func ollamaGenerate(ctx context.Context, cfg *config.LLMConfig, client *http.Client, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":  cfg.Ollama.Model,
		"prompt": prompt,
		"stream": false,
		"format": "json",
	})
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.Ollama.URL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var out struct {
		Response string `json:"response"`
	}
	if err := doJSON(client, req, &out); err != nil {
		return "", fmt.Errorf("eval: ollama generate: %w", err)
	}
	return out.Response, nil
}

// openAIComplete 呼叫 OpenAI 相容的 /chat/completions
func openAIComplete(ctx context.Context, cfg *config.LLMConfig, client *http.Client, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": cfg.API.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": cfg.API.MaxTokens,
	})
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.API.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.API.ResolvedAPIKey())

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := doJSON(client, req, &out); err != nil {
		return "", fmt.Errorf("eval: chat/completions: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("eval: chat/completions returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// anthropicComplete 呼叫 Anthropic /messages
func anthropicComplete(ctx context.Context, cfg *config.LLMConfig, client *http.Client, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":      cfg.API.Model,
		"max_tokens": cfg.API.MaxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", cfg.API.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.API.ResolvedAPIKey())
	req.Header.Set("anthropic-version", "2023-06-01")

	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := doJSON(client, req, &out); err != nil {
		return "", fmt.Errorf("eval: anthropic messages: %w", err)
	}
	if len(out.Content) == 0 {
		return "", fmt.Errorf("eval: anthropic messages returned no content")
	}
	return out.Content[0].Text, nil
}

// embedText 取得文字的 embedding 向量（Ollama /api/embeddings）。
// api 模式走 OpenAI 相容 /embeddings
func embedText(ctx context.Context, cfg *config.LLMConfig, client *http.Client, text string) ([]float64, error) {
	if cfg == nil || !cfg.IsEnabled() {
		return nil, fmt.Errorf("eval: LLM config is nil or mode=none")
	}
	if client == nil {
		client = httpClientFor(cfg)
	}

	switch cfg.Mode {
	case config.LLMModeOllama:
		body, _ := json.Marshal(map[string]string{
			"model":  cfg.Ollama.Model,
			"prompt": text,
		})
		req, err := http.NewRequestWithContext(ctx, "POST", cfg.Ollama.URL+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		var out struct {
			Embedding []float64 `json:"embedding"`
		}
		if err := doJSON(client, req, &out); err != nil {
			return nil, fmt.Errorf("eval: ollama embeddings: %w", err)
		}
		return out.Embedding, nil

	case config.LLMModeAPI:
		body, _ := json.Marshal(map[string]interface{}{
			"model": cfg.API.Model,
			"input": text,
		})
		req, err := http.NewRequestWithContext(ctx, "POST", cfg.API.BaseURL+"/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.API.ResolvedAPIKey())
		var out struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		if err := doJSON(client, req, &out); err != nil {
			return nil, fmt.Errorf("eval: embeddings: %w", err)
		}
		if len(out.Data) == 0 {
			return nil, fmt.Errorf("eval: embeddings returned no data")
		}
		return out.Data[0].Embedding, nil

	default:
		return nil, fmt.Errorf("eval: unsupported llm mode %q", cfg.Mode)
	}
}

// doJSON 送出請求並將 2xx 回應解碼進 out；非 2xx 回傳含 body 摘要的錯誤
func doJSON(client *http.Client, req *http.Request, out interface{}) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := string(data)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet)
	}
	return json.Unmarshal(data, out)
}
