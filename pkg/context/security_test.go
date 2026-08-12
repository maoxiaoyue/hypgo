package context

import (
	"net/http/httptest"
	"testing"
)

// TestClientIPIgnoresForgedHeadersWhenUntrusted 回歸測試（安全）：
// 直連來源不是可信代理時，X-Forwarded-For / X-Real-IP 一律不採信。
//
// 歷史 bug：ClientIP 無條件優先採信 XFF。IPWhitelist 與 RateLimiter
// 都以 ClientIP 為 key，攻擊者自帶 X-Forwarded-For 即可偽裝成白名單
// 內網 IP（繞過白名單），或每次換一個值讓限流 key 永不重複（繞過限流）。
func TestClientIPIgnoresForgedHeadersWhenUntrusted(t *testing.T) {
	SetTrustedProxies() // 不信任任何代理（安全預設）

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:1234" // 攻擊者的真實位址
	req.Header.Set("X-Forwarded-For", "10.0.0.5")
	req.Header.Set("X-Real-IP", "10.0.0.5")

	c := AcquireContext(httptest.NewRecorder(), req)
	defer c.Release()

	if got := c.ClientIP(); got != "203.0.113.9" {
		t.Errorf("ClientIP() = %q, want 203.0.113.9 (forged headers must be ignored)", got)
	}
	if c.IsFromTrustedProxy() {
		t.Error("IsFromTrustedProxy() should be false when no proxies are configured")
	}
}

// TestClientIPUsesForwardedWhenTrusted 直連來源為可信代理時，
// 應解析 XFF 並取最靠近我方、且非代理鏈本身的那一跳。
func TestClientIPUsesForwardedWhenTrusted(t *testing.T) {
	if err := SetTrustedProxies("10.0.0.0/8", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	defer SetTrustedProxies()

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:5678" // nginx（可信）
	// 真實客戶端 → 外部代理 → 我方 nginx
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7, 10.0.0.2")

	c := AcquireContext(httptest.NewRecorder(), req)
	defer c.Release()

	// 10.0.0.2 屬可信網段須跳過，取右邊數來第一個非可信位址
	if got := c.ClientIP(); got != "198.51.100.7" {
		t.Errorf("ClientIP() = %q, want 198.51.100.7", got)
	}
	if !c.IsFromTrustedProxy() {
		t.Error("IsFromTrustedProxy() should be true for a configured proxy")
	}
}

// TestSetTrustedProxiesRejectsGarbage 設定值無效時應回錯，不可靜默忽略
func TestSetTrustedProxiesRejectsGarbage(t *testing.T) {
	defer SetTrustedProxies()
	if err := SetTrustedProxies("not-an-ip"); err == nil {
		t.Error("SetTrustedProxies should reject an invalid entry")
	}
	if err := SetTrustedProxies("10.0.0.0/99"); err == nil {
		t.Error("SetTrustedProxies should reject an invalid CIDR")
	}
}
