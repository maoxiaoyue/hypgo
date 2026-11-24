package hidb

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	hypcontext "github.com/maoxiaoyue/hypgo/pkg/context"
	"github.com/maoxiaoyue/hypgo/pkg/middleware"
)

// 寫後讀（read-your-writes）：複寫是非同步的，寫入後立刻從副本讀，可能讀不到剛寫的資料。
//
//   - 同一個請求內：寫入之後，這個請求剩下的讀取一律走主庫
//   - 跨請求：寫入後的一段時間內（預設 1 秒，database.read_your_writes），同一個使用者的讀取走主庫
//
// 兩者都需要掛上 (*Database).Middleware；沒掛時 ReadHypDB 的查詢照常走副本。
// 跨請求的紀錄存在行程內：多個實例部署時，只有處理那次寫入的實例知道要走主庫

type (
	sessionKey struct{}
	masterKey  struct{}
)

// session 為單一請求的寫後讀狀態
type session struct {
	wrote atomic.Bool
	key   func() string // 延後計算：JWT 中間件可能排在本中間件之後
}

// UseMaster 回傳的 context 讓經由它執行的 ReadHypDB 查詢走主庫（需要最新資料的讀取）
func UseMaster(ctx context.Context) context.Context {
	return context.WithValue(ctx, masterKey{}, true)
}

// defaultSessionKey 以已驗證的使用者（JWT 的 sub）辨識。沒有登入時回空字串：只做同一個請求內的寫後讀。
// 先前改用客戶端 IP，而 trusted_proxies 沒設時那是負載平衡器的位址——所有匿名訪客共用一個鍵，
// 一位訪客寫入後其他人的讀取全走主庫
func defaultSessionKey(c *hypcontext.Context) string {
	if claims, ok := middleware.JWTClaimsFrom(c, ""); ok && claims.Subject != "" {
		return "u:" + claims.Subject
	}
	return ""
}

// Middleware 啟用寫後讀：把請求的狀態放進 c.Request 的 context，
// 以 c 或 c.Request.Context() 執行的查詢都看得到。
// keyFn 自訂「同一個使用者」的辨識方式（跨請求用）；省略時為 JWT 的 sub。回空字串表示這個請求
// 不做跨請求的寫後讀（未登入的請求預設如此；要以 session cookie 或 IP 辨識時自己提供 keyFn）
func (d *Database) Middleware(keyFn ...func(*hypcontext.Context) string) hypcontext.HandlerFunc {
	key := defaultSessionKey
	if len(keyFn) > 0 && keyFn[0] != nil {
		key = keyFn[0]
	}
	return func(c *hypcontext.Context) {
		s := &session{}
		var once sync.Once
		var k string
		s.key = func() string {
			once.Do(func() { k = key(c) })
			return k
		}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), sessionKey{}, s))
		c.Next()
		// handler 返回後 c 會被放回池子重複使用：在這裡定下鍵，之後才執行的查詢
		// （handler 啟動的 goroutine）拿到的是這個值，不再碰 c
		s.key()
	}
}

// recentWrites 記錄每個使用者最近一次寫入的時間（跨請求的寫後讀）
type recentWrites struct {
	m      sync.Map // key → UnixNano
	writes atomic.Uint64
}

// markWrite 記錄 ctx 所屬的請求（與使用者）剛寫入過
func (d *Database) markWrite(ctx context.Context) {
	s, _ := ctx.Value(sessionKey{}).(*session)
	if s == nil {
		return
	}
	s.wrote.Store(true)
	if d.set.readYourWrites <= 0 {
		return
	}
	key := s.key()
	if key == "" {
		return
	}
	now := time.Now().UnixNano()
	d.recent.m.Store(key, now)
	// 每 1024 次寫入清一次過期的紀錄，表不會無限成長
	if d.recent.writes.Add(1)%1024 == 0 {
		d.recent.m.Range(func(k, v interface{}) bool {
			if now-v.(int64) > int64(d.set.readYourWrites) {
				d.recent.m.Delete(k)
			}
			return true
		})
	}
}

// needsMaster 回報 ctx 的讀取是否該走主庫：明確要求、這個請求寫入過，或這位使用者剛寫入過
func (d *Database) needsMaster(ctx context.Context) bool {
	if ctx.Value(masterKey{}) != nil {
		return true
	}
	s, _ := ctx.Value(sessionKey{}).(*session)
	if s == nil {
		return false
	}
	if s.wrote.Load() {
		return true
	}
	if d.set.readYourWrites <= 0 {
		return false
	}
	key := s.key()
	if key == "" {
		return false
	}
	if v, ok := d.recent.m.Load(key); ok {
		return time.Now().UnixNano()-v.(int64) < int64(d.set.readYourWrites)
	}
	return false
}
