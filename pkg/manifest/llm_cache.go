package manifest

// llm_cache.go — LLM 增強結果快取：路由送給 LLM 的 prompt 沒變，就沿用上次的結果、不再請求。
// 重新生成 manifest 時多數路由並未改動，先前每次啟動都對每條路由重打一次 LLM（API 模式逐次計費）。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
)

// DefaultLLMCacheFilename LLM 增強結果快取在 .hyp/ 內的檔名（刪掉即強制全部重新產生）
const DefaultLLMCacheFilename = "llm_cache.json"

// llmCacheEntry LLM 為一條路由回填的欄位
type llmCacheEntry struct {
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
}

// cachingEnricher 包住實際的 LLMEnricher，以「模型 + 完整 prompt」的雜湊為鍵快取結果。
// prompt 涵蓋 LLM 看得到的一切（路由欄位、handler、tags、人寫的 summary），任一變動、
// 換模型或改了 prompt 程式碼都會讓鍵改變而重新請求，不需另外維護比對欄位。
// 只快取成功的結果（LLM 確實回填了欄位），失敗的路由下次照樣重試
type cachingEnricher struct {
	inner LLMEnricher
	model string

	mu     sync.Mutex
	prev   map[string]llmCacheEntry // 上次存下的快取
	next   map[string]llmCacheEntry // 本次用到或新產生的；存檔只留這些，已刪除路由的條目自然淘汰
	hits   int
	misses int
}

func newCachingEnricher(inner LLMEnricher, model string, prev map[string]llmCacheEntry) *cachingEnricher {
	return &cachingEnricher{inner: inner, model: model, prev: prev, next: make(map[string]llmCacheEntry)}
}

// EnrichRoute 命中快取時直接回填，否則呼叫實際的 enricher 並記下結果
func (e *cachingEnricher) EnrichRoute(route RouteManifest) RouteManifest {
	sum := sha256.Sum256([]byte(e.model + "\x00" + buildEnrichPrompt(route)))
	key := hex.EncodeToString(sum[:])

	e.mu.Lock()
	entry, ok := e.prev[key]
	if ok {
		e.next[key] = entry
		e.hits++
	}
	e.mu.Unlock()
	if ok {
		// 與 parseEnrichResponse 相同：只回填空白欄位
		if route.Summary == "" {
			route.Summary = entry.Summary
		}
		if route.Description == "" {
			route.Description = entry.Description
		}
		return route
	}

	out := e.inner.EnrichRoute(route)
	var got llmCacheEntry
	if route.Summary == "" {
		got.Summary = out.Summary
	}
	if route.Description == "" {
		got.Description = out.Description
	}

	e.mu.Lock()
	e.misses++
	if got != (llmCacheEntry{}) {
		e.next[key] = got
	}
	e.mu.Unlock()
	return out
}

// stats 回傳本次命中與實際請求的路由數
func (e *cachingEnricher) stats() (hits, misses int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits, e.misses
}

// loadLLMCache 讀取快取檔；不存在或格式損壞時回傳空表（等同全部重新產生）
func loadLLMCache(path string) map[string]llmCacheEntry {
	entries := make(map[string]llmCacheEntry)
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &entries) != nil {
			return make(map[string]llmCacheEntry)
		}
	}
	return entries
}

// save 寫入本次用到的條目；內容沒變就不寫。map 序列化依鍵排序，輸出穩定
func (e *cachingEnricher) save(path string) error {
	e.mu.Lock()
	data, err := json.MarshalIndent(e.next, "", "  ")
	e.mu.Unlock()
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
