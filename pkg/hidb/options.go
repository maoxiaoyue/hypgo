package hidb

import (
	"time"

	"github.com/maoxiaoyue/hypgo/pkg/config"
	"github.com/maoxiaoyue/hypgo/pkg/logger"
	"github.com/uptrace/bun"
)

// 行為設定的預設值（config.yaml 的 database 區段或對應的 Option 可調整）
const (
	DefaultSlowQuery       = 500 * time.Millisecond
	DefaultConnMaxLifetime = 30 * time.Minute
)

// settings 為解析後的行為設定：Option 優先，其次設定檔，最後預設值
type settings struct {
	slowQuery       time.Duration // 0 = 不記慢查詢
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
	strictColumns   bool
}

// overrides 記錄 Option 明確指定的值（nil = 沒指定）
type overrides struct {
	slowQuery *time.Duration
	bunOpts   []bun.DBOption
	logger    *logger.Logger
}

// WithLogger 指定 hidb 的日誌（慢查詢、副本摘除與恢復）。未指定時寫到標準輸出
func WithLogger(l *logger.Logger) Option {
	return func(db *Database) { db.over.logger = l }
}

// WithBunOptions 傳入建立 bun.DB 的選項（主庫與每個副本都套用）。
// 先前 NewWithInterface 不接受任何 bun 選項，bun.WithDiscardUnknownColumns 這類設定開不了
func WithBunOptions(opts ...bun.DBOption) Option {
	return func(db *Database) { db.over.bunOpts = append(db.over.bunOpts, opts...) }
}

// WithSlowQuery 設定慢查詢門檻（預設 500ms）；0 或負數關閉
func WithSlowQuery(threshold time.Duration) Option {
	return func(db *Database) { db.over.slowQuery = &threshold }
}

// resolveSettings 合併 Option、設定檔與預設值
func (d *Database) resolveSettings() {
	var o config.DatabaseOptions
	if p, ok := d.config.(config.DatabaseOptionsProvider); ok {
		o = p.GetDatabaseOptions()
	}
	// 設定檔的數值：0 = 沒設定（用預設值），負數 = 關閉
	pick := func(v int, unit, def time.Duration) time.Duration {
		switch {
		case v > 0:
			return time.Duration(v) * unit
		case v < 0:
			return 0
		}
		return def
	}
	s := settings{
		slowQuery:       pick(o.SlowQuery, time.Millisecond, DefaultSlowQuery),
		connMaxLifetime: pick(o.ConnMaxLifetime, time.Second, DefaultConnMaxLifetime),
		connMaxIdleTime: pick(o.ConnMaxIdleTime, time.Second, 0),
		strictColumns:   o.StrictColumns,
	}

	if v := d.over.slowQuery; v != nil {
		s.slowQuery = max(*v, 0)
	}
	d.set = s

	d.log = d.over.logger
	if d.log == nil {
		d.log = logger.NewLogger()
	}
}

// bunOptions 回傳建立 bun.DB 的選項：預設忽略 model 沒有的欄位，使用者的選項在後（可覆寫）
func (d *Database) bunOptions() []bun.DBOption {
	var opts []bun.DBOption
	if !d.set.strictColumns {
		// 先加欄位、再滾動更新服務的期間，舊版服務的 SELECT * 會遇到它不認得的欄位。
		// bun 預設回錯（"does not have column"），那段時間這類查詢全部失敗
		opts = append(opts, bun.WithDiscardUnknownColumns())
	}
	return append(opts, d.over.bunOpts...)
}
