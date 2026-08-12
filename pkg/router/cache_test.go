package router

import (
	"sync"
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func TestRouteCache(t *testing.T) {
	cache := newRouteCache(2)

	dummyHandler := func(c *hypcontext.Context) {}

	// 空快取查找
	if handlers := cache.get("/test"); handlers != nil {
		t.Errorf("Expected nil for non-existent key")
	}

	// Put + Get
	cache.put("/a", []hypcontext.HandlerFunc{dummyHandler})
	if handlers := cache.get("/a"); handlers == nil {
		t.Errorf("Expected handlers for key '/a'")
	}

	// 覆寫既有 key
	cache.put("/a", []hypcontext.HandlerFunc{dummyHandler, dummyHandler})
	if handlers := cache.get("/a"); len(handlers) != 2 {
		t.Errorf("Expected 2 handlers after update, got %d", len(handlers))
	}
}

// TestRouteCacheConcurrentGetPut 回歸測試：舊 LRU 實作中，get 解鎖後回傳的
// *cacheItem 可能被並發淘汰路徑清空（handlers=nil）並回收進 pool，
// 讀取方會執行到 nil/錯誤的 handler 鏈。sync.Map 版不可能發生；
// 本測試在 -race 下驗證並發 get/put 無 race、命中結果永不為空
func TestRouteCacheConcurrentGetPut(t *testing.T) {
	cache := newRouteCache(4)
	dummyHandler := func(c *hypcontext.Context) {}
	keys := []string{"GET/a", "GET/b", "GET/c", "GET/d", "GET/e", "GET/f"}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				cache.put(keys[n%len(keys)], []hypcontext.HandlerFunc{dummyHandler})
			}
		}()
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				if h := cache.get(keys[n%len(keys)]); h != nil && len(h) == 0 {
					t.Error("cache hit returned empty handler chain")
					return
				}
			}
		}()
	}
	wg.Wait()
}
