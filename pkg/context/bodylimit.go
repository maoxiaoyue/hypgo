package context

import (
	"io"
	"net/http"
)

// limitedBody 有上限的請求 body：讀超過 limit 位元組時回 *http.MaxBytesError。
// 與 http.MaxBytesReader 的差別是上限可以事後調整（SetBodyLimit），
// 讓 server 套用的全域上限能被個別路由放寬或收緊
type limitedBody struct {
	rc       io.ReadCloser
	limit    int64
	n        int64 // 已讀位元組
	declared int64 // 請求的 Content-Length；未知為 -1
}

func (b *limitedBody) Read(p []byte) (int, error) {
	// Content-Length 已經超過上限時不讀：先前仍先讀 limit+1 位元組才拒絕，
	// 帶 Expect: 100-continue 的客戶端也因此被要求上傳（net/http 在第一次讀 body 時送 100）
	if b.n > b.limit || b.declared > b.limit {
		return 0, &http.MaxBytesError{Limit: b.limit}
	}
	// 至多讀到 limit+1：多出的那一個位元組用來分辨「剛好等於上限」與「超過」
	if rem := b.limit - b.n + 1; int64(len(p)) > rem {
		p = p[:rem]
	}
	n, err := b.rc.Read(p)
	b.n += int64(n)
	if b.n > b.limit {
		return n - int(b.n-b.limit), &http.MaxBytesError{Limit: b.limit}
	}
	return n, err
}

func (b *limitedBody) Close() error { return b.rc.Close() }

// LimitRequestBody 把請求 body 的大小限制在 limit 位元組；已有上限時改成新的值。
// 超過時讀取端收到 *http.MaxBytesError（BindInput 會回 413）。limit <= 0 不做事。
// server 以 server.max_body_size 對每個請求呼叫；先前沒有任何預設上限，
// 一個 300MB 的 JSON 字串會被整包讀進記憶體
func LimitRequestBody(r *http.Request, limit int64) {
	if r == nil || r.Body == nil || r.Body == http.NoBody || limit <= 0 {
		return
	}
	if b, ok := r.Body.(*limitedBody); ok {
		b.limit = limit
		return
	}
	r.Body = &limitedBody{rc: r.Body, limit: limit, declared: r.ContentLength}
}

// SetBodyLimit 調整這個請求的 body 上限（位元組），要在讀取 body 之前呼叫。
// 上傳大檔的路由用它放寬 server.max_body_size，也可以用 middleware.BodyLimit
func (c *Context) SetBodyLimit(limit int64) {
	LimitRequestBody(c.Request, limit)
}
