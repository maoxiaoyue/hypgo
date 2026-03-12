package pg

import (
	_ "github.com/lib/pq"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/schema"
)

// Dialect PostgreSQL 方言實現
type Dialect struct{}

// New 創建 PostgreSQL 方言實例
func New() *Dialect {
	return &Dialect{}
}

// DriverName 返回 sql.Open 使用的驅動名稱
func (d *Dialect) DriverName() string {
	return "postgres"
}

// BunDialect 返回 bun ORM 使用的方言實例
func (d *Dialect) BunDialect() schema.Dialect {
	return pgdialect.New()
}
