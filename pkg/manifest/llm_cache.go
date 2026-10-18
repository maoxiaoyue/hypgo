package manifest

// llm_cache.go — LLM 增強結果快取：路由送給 LLM 的內容沒變，就沿用上次的結果、不再請求。
// 重新生成 manifest 時多數路由並未改動，先前每次啟動都對每條路由重打一次 LLM（API 模式逐次計費）。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
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

// cachingEnricher 包住實際的 LLMEnricher，以「模型 + 指令 + 該路由的 prompt 區塊」的雜湊為鍵快取結果。
// 區塊涵蓋 LLM 看得到的一切（路由欄位、handler、tags、人寫的 summary），任一變動、
// 換模型或改了指令都會讓鍵改變而重新請求，不需另外維護比對欄位；同批還有哪些路由不影響鍵。
// 只快取成功的結果（LLM 確實回填了欄位），失敗的路由下次照樣重試
type cachingEnricher struct {
	inner LLMEnricher
	model string
	path  string

	mu     sync.Mutex
	prev   map[string]llmCacheEntry // 上次存下的快取
	next   map[string]llmCacheEntry // 本次用到或新產生的；最終存檔只留這些，已刪除路由的條目自然淘汰
	hits   int
	misses int
}

// newCachingEnricher 由 path 載入上次的快取；path 為空時不讀寫檔案
func newCachingEnricher(inner LLMEnricher, model, path string) *cachingEnricher {
	prev := make(map[string]llmCacheEntry)
	if path != "" {
		prev = loadLLMCache(path)
	}
	return &cachingEnricher{inner: inner, model: model, path: path, prev: prev, next: make(map[string]llmCacheEntry)}
}

func (e *cachingEnricher) key(route RouteManifest) string {
	sum := sha256.Sum256([]byte(e.model + "\x00" + enrichInstruction + "\x00" + routePromptBlock(route)))
	return hex.EncodeToString(sum[:])
}

// EnrichRoutes 命中快取的路由直接回填；其餘每 llmBatchSize 條交給實際的 enricher 一次（一個請求），
// 每批完成就存檔：背景同步被中斷時（server 停止、hyp run 熱重載），已完成的批次不必重做。
// 先前只在全部路由跑完後存一次，開發中頻繁重啟時快取幾乎累積不起來
func (e *cachingEnricher) EnrichRoutes(routes []RouteManifest) {
	keys := make([]string, len(routes))
	var misses []int
	e.mu.Lock()
	for i := range routes {
		keys[i] = e.key(routes[i])
		entry, ok := e.prev[keys[i]]
		if !ok {
			misses = append(misses, i)
			continue
		}
		e.next[keys[i]] = entry
		e.hits++
		// 與 applyEnrichResponse 相同：只回填空白欄位
		if routes[i].Summary == "" {
			routes[i].Summary = entry.Summary
		}
		if routes[i].Description == "" {
			routes[i].Description = entry.Description
		}
	}
	e.mu.Unlock()

	for start := 0; start < len(misses); start += llmBatchSize {
		idx := misses[start:min(start+llmBatchSize, len(misses))]
		batch := make([]RouteManifest, len(idx))
		for j, i := range idx {
			batch[j] = routes[i]
		}
		e.inner.EnrichRoutes(batch)

		e.mu.Lock()
		for j, i := range idx {
			var got llmCacheEntry
			if routes[i].Summary == "" {
				got.Summary = batch[j].Summary
			}
			if routes[i].Description == "" {
				got.Description = batch[j].Description
			}
			if got != (llmCacheEntry{}) {
				e.next[keys[i]] = got
			}
			e.misses++
			routes[i] = batch[j]
		}
		e.mu.Unlock()
		e.write(false) // 中途存檔失敗不影響本次結果；最終的 save 會回報錯誤
	}
}

// stats 回傳本次命中快取與實際送給 LLM 的路由數
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

// save 於同步結束時呼叫：只寫入本次用到的條目，已刪除路由的舊條目就此淘汰
func (e *cachingEnricher) save() error {
	return e.write(true)
}

// write 寫入快取檔；內容沒變就不寫。prune 為 false（同步途中）時連同上次的條目一起寫，
// 這時還沒跑到的路由不能先被淘汰。map 序列化依鍵排序，輸出穩定
func (e *cachingEnricher) write(prune bool) error {
	if e.path == "" {
		return nil
	}
	e.mu.Lock()
	entries := e.next
	if !prune {
		entries = maps.Clone(e.prev)
		maps.Copy(entries, e.next)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	e.mu.Unlock()
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if old, err := os.ReadFile(e.path); err == nil && bytes.Equal(old, data) {
		return nil
	}

	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, e.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
