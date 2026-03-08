package mysql

import (
	_ "github.com/go-sql-driver/mysql"
	"github.com/uptrace/bun/dialect/mysqldialect"
	"github.com/uptrace/bun/schema"
)

// Dialect MySQL/TiDB 方言實現
// TiDB 兼容 MySQL 協議，使用者同樣用 mysql.New()
type Dialect struct{}

// New 創建 MySQL 方言實例
func New() *Dialect {
	return &Dialect{}
}

// DriverName 返回 sql.Open 使用的驅動名稱
func (d *Dialect) DriverName() string {
	return "mysql"
}

// BunDialect 返回 bun ORM 使用的方言實例
func (d *Dialect) BunDialect() schema.Dialect {
	return mysqldialect.New()
}
