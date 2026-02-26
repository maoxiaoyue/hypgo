package router

import (
	"testing"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
)

func TestRouteCache(t *testing.T) {
	cache := newRouteCache(2)

	dummyHandler := func(c *hypcontext.Context) {}

	// Test Get on empty cache
	if entry := cache.get("/test"); entry != nil {
		t.Errorf("Expected nil for non-existent key")
	}

	// Test Put
	cache.put("/a", []hypcontext.HandlerFunc{dummyHandler}, nil)
	if entry := cache.get("/a"); entry == nil {
		t.Errorf("Expected entry for key '/a'")
	}

	// Test Capacity and Eviction (LRU)
	cache.put("/b", []hypcontext.HandlerFunc{dummyHandler}, nil)
	cache.put("/c", []hypcontext.HandlerFunc{dummyHandler}, nil)

	// Since capacity is 2, "/a" should be evicted
	if entry := cache.get("/a"); entry != nil {
		t.Errorf("Expected '/a' to be evicted")
	}

	if entry := cache.get("/b"); entry == nil {
		t.Errorf("Expected entry for key '/b'")
	}
	if entry := cache.get("/c"); entry == nil {
		t.Errorf("Expected entry for key '/c'")
	}

	// Test LRU update on Get
	cache.get("/b")                                              // "/b" is now recently used
	cache.put("/d", []hypcontext.HandlerFunc{dummyHandler}, nil) // should evict "/c"

	if entry := cache.get("/c"); entry != nil {
		t.Errorf("Expected '/c' to be evicted")
	}
	if entry := cache.get("/b"); entry == nil {
		t.Errorf("Expected entry for key '/b'")
	}

	// Test Update existing key
	cache.put("/b", []hypcontext.HandlerFunc{dummyHandler, dummyHandler}, nil)
	entry := cache.get("/b")
	if len(entry.handlers) != 2 {
		t.Errorf("Expected entry to be updated with 2 handlers")
	}
}
