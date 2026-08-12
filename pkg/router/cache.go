// @chris
package router

import (
	"sync"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// routeCache 靜態路由快取
//
// 只有無參數的靜態路由會被放入（見 ServeHTTP），鍵空間在路由註冊完成後
// 即固定且有上限，因此不需要 LRU 淘汰。舊實作是「RWMutex + 雙向鏈表 LRU +
// cacheItem pool」：每次命中都要拿全域寫鎖搬動鏈表節點（高並發下成為
// 全域競爭點，且比無鎖的 radix tree 靜態查找更慢），而 get 在解鎖後
// 回傳的 *cacheItem 可能被並發的淘汰路徑清空並回收進 pool——
// use-after-recycle race。改用 sync.Map 後：命中路徑無鎖、無分配、
// 值寫入後不再變動，race 隨結構消失。
type routeCache struct {
	items sync.Map // key: method+path → []hypcontext.HandlerFunc
}

// newRouteCache 創建路由快取。capacity 保留參數以維持既有呼叫介面；
// 鍵空間受註冊的靜態路由數量自然限制，無需容量控制
func newRouteCache(capacity int) *routeCache {
	return &routeCache{}
}

// get 取出靜態路由的 handler 鏈；未命中回傳 nil
func (c *routeCache) get(key string) []hypcontext.HandlerFunc {
	if v, ok := c.items.Load(key); ok {
		return v.([]hypcontext.HandlerFunc)
	}
	return nil
}

// put 放入靜態路由的 handler 鏈（同 key 重複放入為冪等覆寫）
func (c *routeCache) put(key string, handlers []hypcontext.HandlerFunc) {
	c.items.Store(key, handlers)
}
