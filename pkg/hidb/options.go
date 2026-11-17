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
	DefaultReadYourWrites  = time.Second
	DefaultTxRetries       = 1
	DefaultConnMaxLifetime = 30 * time.Minute
)

// settings 為解析後的行為設定：Option 優先，其次設定檔，最後預設值
type settings struct {
	slowQuery       time.Duration // 0 = 不記慢查詢
	readYourWrites  time.Duration // 0 = 不做跨請求的寫後讀
	maxReplicaLag   time.Duration // 0 = 不檢查複寫延遲
	txRetries       int
	connMaxLifetime time.Duration
	connMaxIdleTime time.Duration
	strictColumns   bool
}

// overrides 記錄 Option 明確指定的值（nil = 沒指定）
type overrides struct {
	slowQuery, readYourWrites, maxReplicaLag *time.Duration
	txRetries                                *int
	bunOpts                                  []bun.DBOption
	logger                                   *logger.Logger
}

// WithLogger 指定 hidb 的日誌（慢查詢、副本摘除與恢復、設定提示）。未指定時寫到標準輸出
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

// WithReadYourWrites 設定寫入後同一使用者的讀取走主庫的時間（預設 1 秒）；0 或負數關閉跨請求的部分。
// 同一個請求內寫入後的讀取一律走主庫，不受此值影響。需要掛上 (*Database).Middleware 才會生效
func WithReadYourWrites(window time.Duration) Option {
	return func(db *Database) { db.over.readYourWrites = &window }
}

// WithMaxReplicaLag 設定可接受的複寫延遲；副本超過時暫不使用，追上後自動恢復。0 或負數不檢查（預設）
func WithMaxReplicaLag(lag time.Duration) Option {
	return func(db *Database) { db.over.maxReplicaLag = &lag }
}

// WithTxRetries 設定交易遇到死結 / 序列化失敗時的重試次數（預設 1）；0 或負數不重試。
// 重試會把交易函式整個再執行一次：函式裡資料庫以外的副作用（寄信、呼叫外部 API）也會再做一次
func WithTxRetries(n int) Option {
	return func(db *Database) { db.over.txRetries = &n }
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
		readYourWrites:  pick(o.ReadYourWrites, time.Millisecond, DefaultReadYourWrites),
		maxReplicaLag:   pick(o.MaxReplicaLag, time.Millisecond, 0),
		connMaxLifetime: pick(o.ConnMaxLifetime, time.Second, DefaultConnMaxLifetime),
		connMaxIdleTime: pick(o.ConnMaxIdleTime, time.Second, 0),
		strictColumns:   o.StrictColumns,
		txRetries:       DefaultTxRetries,
	}
	switch {
	case o.TxRetries > 0:
		s.txRetries = o.TxRetries
	case o.TxRetries < 0:
		s.txRetries = 0
	}

	if v := d.over.slowQuery; v != nil {
		s.slowQuery = max(*v, 0)
	}
	if v := d.over.readYourWrites; v != nil {
		s.readYourWrites = max(*v, 0)
	}
	if v := d.over.maxReplicaLag; v != nil {
		s.maxReplicaLag = max(*v, 0)
	}
	if v := d.over.txRetries; v != nil {
		s.txRetries = max(*v, 0)
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
