package migrate

import (
	"fmt"
	"strings"
	"time"
)

// GenerateSQL 根據 ChangeSet 產生 up/down SQL
// dialect: "postgres" 或 "mysql"
func GenerateSQL(changes []Change, dialect string) (up, down string) {
	if len(changes) == 0 {
		return "", ""
	}

	gen := &sqlGenerator{dialect: dialect}
	var upLines, downLines []string

	for _, c := range changes {
		u, d := gen.generate(c)
		if u != "" {
			upLines = append(upLines, u)
		}
		if d != "" {
			downLines = append(downLines, d)
		}
	}

	return strings.Join(upLines, "\n"), strings.Join(downLines, "\n")
}

// MigrationFiles 回傳建議的檔案名稱
func MigrationFiles(prefix string) (upFile, downFile string) {
	ts := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s%s_auto.up.sql", prefix, ts),
		fmt.Sprintf("%s%s_auto.down.sql", prefix, ts)
}

type sqlGenerator struct {
	dialect string
}

func (g *sqlGenerator) generate(c Change) (up, down string) {
	switch c.Type {
	case AddTable:
		// AddTable 只產出 CREATE/DROP，欄位由後續 AddColumn 處理
		// 但為了產出完整 CREATE TABLE，需收集所有 AddColumn
		// 這裡只回傳空，由 GenerateCreateTable 處理
		return "", fmt.Sprintf("DROP TABLE IF EXISTS %s;", g.quote(c.Table))

	case DropTable:
		return fmt.Sprintf("DROP TABLE IF EXISTS %s;", g.quote(c.Table)), ""

	case AddColumn:
		colDef := g.columnDefinition(c.Column)
		up = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
			g.quote(c.Table), g.quote(c.Column.Name), colDef)
		down = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
			g.quote(c.Table), g.quote(c.Column.Name))
		return up, down

	case DropColumn:
		up = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
			g.quote(c.Table), g.quote(c.Column.Name))
		down = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s;",
			g.quote(c.Table), g.quote(c.Column.Name), g.columnDefinition(c.Column))
		return up, down

	case AlterColumn:
		return g.generateAlter(c)

	default:
		return "", ""
	}
}

func (g *sqlGenerator) generateAlter(c Change) (up, down string) {
	if c.Column == nil {
		return "", ""
	}

	var upParts, downParts []string
	table := g.quote(c.Table)
	col := g.quote(c.Column.Name)

	// 型別變更
	if c.OldCol != nil && g.sqlType(c.Column) != g.sqlType(c.OldCol) {
		if g.dialect == "postgres" {
			upParts = append(upParts,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;", table, col, g.sqlType(c.Column)))
			downParts = append(downParts,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;", table, col, g.sqlType(c.OldCol)))
		} else {
			upParts = append(upParts,
				fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s;", table, col, g.columnDefinition(c.Column)))
			downParts = append(downParts,
				fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s;", table, col, g.columnDefinition(c.OldCol)))
		}
	}

	// NOT NULL 變更
	if c.OldCol != nil && c.Column.NotNull != c.OldCol.NotNull {
		if g.dialect == "postgres" {
			if c.Column.NotNull {
				upParts = append(upParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", table, col))
				downParts = append(downParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", table, col))
			} else {
				upParts = append(upParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", table, col))
				downParts = append(downParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", table, col))
			}
		}
	}

	// DEFAULT 變更
	if c.OldCol != nil && c.Column.Default != c.OldCol.Default {
		if g.dialect == "postgres" {
			if c.Column.Default != "" {
				upParts = append(upParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", table, col, c.Column.Default))
			} else {
				upParts = append(upParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", table, col))
			}
			if c.OldCol.Default != "" {
				downParts = append(downParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s;", table, col, c.OldCol.Default))
			} else {
				downParts = append(downParts,
					fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", table, col))
			}
		}
	}

	return strings.Join(upParts, "\n"), strings.Join(downParts, "\n")
}

// columnDefinition 產生欄位的 SQL 定義（不含欄位名）
func (g *sqlGenerator) columnDefinition(col *ColumnSchema) string {
	var parts []string
	parts = append(parts, g.sqlType(col))

	if col.PrimaryKey {
		parts = append(parts, "PRIMARY KEY")
	}
	if col.NotNull {
		parts = append(parts, "NOT NULL")
	}
	if col.Unique {
		parts = append(parts, "UNIQUE")
	}
	if col.Default != "" {
		parts = append(parts, "DEFAULT "+col.Default)
	}

	return strings.Join(parts, " ")
}

// sqlType 將 Go 型別（或明確 SQL type）轉為 SQL 型別
func (g *sqlGenerator) sqlType(col *ColumnSchema) string {
	if col.SQLType != "" {
		return strings.ToUpper(col.SQLType)
	}

	isPostgres := g.dialect == "postgres"

	if col.PrimaryKey && col.AutoIncrement {
		if isPostgres {
			if col.GoType == "int64" {
				return "BIGSERIAL"
			}
			return "SERIAL"
		}
		if col.GoType == "int64" {
			return "BIGINT AUTO_INCREMENT"
		}
		return "INT AUTO_INCREMENT"
	}

	switch col.GoType {
	case "int64":
		return "BIGINT"
	case "int", "int32":
		return "INTEGER"
	case "int16":
		return "SMALLINT"
	case "float64":
		if isPostgres {
			return "DOUBLE PRECISION"
		}
		return "DOUBLE"
	case "float32":
		return "REAL"
	case "bool":
		return "BOOLEAN"
	case "string":
		return "TEXT"
	case "time.Time":
		if isPostgres {
			return "TIMESTAMPTZ"
		}
		return "DATETIME"
	default:
		return "TEXT"
	}
}

// quote 用引號包裹識別符
func (g *sqlGenerator) quote(name string) string {
	if g.dialect == "mysql" {
		return "`" + name + "`"
	}
	return `"` + name + `"`
}
