package router

import (
	"sync"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

// routeCache LRU 路由快取
// 用於快取靜態路由的查找結果，減少 Radix Tree 遍歷
type routeCache struct {
	mu       sync.RWMutex
	items    map[string]*cacheItem
	head     *cacheItem // 最近使用
	tail     *cacheItem // 最久未使用
	capacity int
	size     int
}

// cacheItem 快取項目（雙向鏈表節點）
type cacheItem struct {
	key      string
	handlers []hypcontext.HandlerFunc
	params   []Param
	prev     *cacheItem
	next     *cacheItem
}

// newRouteCache 創建路由快取
func newRouteCache(capacity int) *routeCache {
	return &routeCache{
		items:    make(map[string]*cacheItem, capacity),
		capacity: capacity,
	}
}

// get 從快取中取出項目（命中時移到頭部）
func (c *routeCache) get(key string) *cacheItem {
	c.mu.RLock()
	entry, exists := c.items[key]
	c.mu.RUnlock()

	if !exists {
		return nil
	}

	// 移到頭部（LRU）
	c.mu.Lock()
	c.moveToHead(entry)
	c.mu.Unlock()

	return entry
}

// put 放入快取（已存在則更新並移到頭部，超容量時淘汰尾部）
func (c *routeCache) put(key string, handlers []hypcontext.HandlerFunc, params []Param) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 已存在 → 更新
	if entry, exists := c.items[key]; exists {
		entry.handlers = handlers
		entry.params = params
		c.moveToHead(entry)
		return
	}

	// 新增項目
	entry := &cacheItem{
		key:      key,
		handlers: handlers,
		params:   params,
	}

	c.items[key] = entry
	c.addToHead(entry)
	c.size++

	// 超過容量 → 淘汰最久未使用的
	if c.size > c.capacity {
		c.removeTail()
	}
}

// 雙向鏈表操作
func (c *routeCache) moveToHead(entry *cacheItem) {
	c.removeEntry(entry)
	c.addToHead(entry)
}

func (c *routeCache) addToHead(entry *cacheItem) {
	entry.prev = nil
	entry.next = c.head

	if c.head != nil {
		c.head.prev = entry
	}
	c.head = entry

	if c.tail == nil {
		c.tail = entry
	}
}

func (c *routeCache) removeEntry(entry *cacheItem) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.head = entry.next
	}

	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.tail = entry.prev
	}
}

func (c *routeCache) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.items, c.tail.key)
	c.removeEntry(c.tail)
	c.size--
}
