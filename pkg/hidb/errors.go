package hidb

import (
	"database/sql"
	"errors"
	"sync"
)

// 資料庫錯誤的分類。以 errors.Is(hidb.Classify(err), hidb.ErrDuplicate) 判斷，
// handler 不必自己解析 *pq.Error / *mysql.MySQLError 的錯誤碼
var (
	ErrNotFound   = errors.New("hidb: no rows")                         // sql.ErrNoRows
	ErrDuplicate  = errors.New("hidb: unique constraint violated")      // 唯一鍵衝突
	ErrForeignKey = errors.New("hidb: foreign key constraint violated") // 外鍵違反
	ErrDeadlock   = errors.New("hidb: deadlock or serialization failure")
)

var (
	classifiersMu sync.RWMutex
	classifiers   []func(error) error
)

// RegisterErrorClassifier 註冊一個分類函式：認得的錯誤回傳上面其中一個分類，否則回 nil。
// hidb/pg 與 hidb/mysql 在匯入時各自註冊（核心不直接依賴任何 driver）
func RegisterErrorClassifier(fn func(error) error) {
	classifiersMu.Lock()
	defer classifiersMu.Unlock()
	classifiers = append(classifiers, fn)
}

// classified 同時是分類與原本的錯誤：errors.Is 兩者皆成立，訊息維持原樣
type classified struct {
	kind error
	err  error
}

func (c *classified) Error() string   { return c.err.Error() }
func (c *classified) Unwrap() []error { return []error{c.kind, c.err} }

// Classify 把資料庫錯誤對應到 ErrNotFound / ErrDuplicate / ErrForeignKey / ErrDeadlock。
// 認得時回傳的錯誤同時滿足 errors.Is(分類) 與 errors.Is / errors.As(原錯誤)；不認得時原樣回傳
func Classify(err error) error {
	if err == nil {
		return nil
	}
	var done *classified
	if errors.As(err, &done) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return &classified{kind: ErrNotFound, err: err}
	}
	classifiersMu.RLock()
	defer classifiersMu.RUnlock()
	for _, fn := range classifiers {
		if kind := fn(err); kind != nil {
			return &classified{kind: kind, err: err}
		}
	}
	return err
}
