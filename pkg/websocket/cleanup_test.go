package websocket

import (
	"fmt"
	"testing"
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/logger"
)

// TestCleanupInactiveClientsNoDeadlock 回歸測試（兩個歷史 bug）：
//
//  1. cleanupInactiveClients 曾把不活躍 client 送進 h.unregister channel，
//     但該 channel 的唯一消費者是 Hub 的 Run goroutine——也就是呼叫
//     cleanupInactiveClients 的同一個 goroutine。一次清掃超過 channel
//     容量（16）個 client 時，第 17 個發送永久阻塞，整個 Hub 凍結。
//  2. Client.reset() 曾用「非阻塞 drain」清空 Send channel，但
//     handleUnregister 會先 close(Send)：closed channel 的接收永不阻塞，
//     drain 迴圈的 default 分支永遠走不到——每次斷線都讓 hub goroutine
//     無限迴圈。
//
// 本測試直接在單一 goroutine 內清理 40 個過期 client（超過 channel 容量），
// 任一 bug 存在都會卡死，由 watchdog 抓出。
func TestCleanupInactiveClientsNoDeadlock(t *testing.T) {
	log := logger.NewLogger()
	hub := NewHub(log, DefaultConfig)

	const staleClients = 40 // > unregister channel 容量（16）
	for i := 0; i < staleClients; i++ {
		c := AcquireClient(fmt.Sprintf("stale-%d", i), nil, hub, codecJSON)
		c.lastActivity = time.Now().Add(-24 * time.Hour)
		hub.handleRegister(c)
	}

	done := make(chan struct{})
	go func() {
		hub.cleanupInactiveClients()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanupInactiveClients 卡死（Hub Run goroutine 將永久凍結）")
	}

	hub.mu.RLock()
	remaining := len(hub.clients)
	hub.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("預期所有過期 client 都被移除，尚餘 %d", remaining)
	}
	if got := hub.stats.ActiveConnections.Load(); got != 0 {
		t.Errorf("ActiveConnections = %d, want 0", got)
	}
}
