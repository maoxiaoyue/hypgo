package migrate

// ChangeType 描述變更類型
type ChangeType int

const (
	AddTable    ChangeType = iota // 新增表
	DropTable                     // 刪除表
	AddColumn                     // 新增欄位
	DropColumn                    // 刪除欄位
	AlterColumn                   // 修改欄位
)

// Change 描述單一 schema 變更
type Change struct {
	Type   ChangeType    `json:"type"`
	Table  string        `json:"table"`
	Column *ColumnSchema `json:"column,omitempty"`
	OldCol *ColumnSchema `json:"old_column,omitempty"` // AlterColumn 時的舊欄位
}

// String 回傳變更的可讀描述
func (c Change) String() string {
	switch c.Type {
	case AddTable:
		return "ADD TABLE " + c.Table
	case DropTable:
		return "DROP TABLE " + c.Table
	case AddColumn:
		if c.Column != nil {
			return "ADD COLUMN " + c.Table + "." + c.Column.Name
		}
		return "ADD COLUMN " + c.Table
	case DropColumn:
		if c.Column != nil {
			return "DROP COLUMN " + c.Table + "." + c.Column.Name
		}
		return "DROP COLUMN " + c.Table
	case AlterColumn:
		if c.Column != nil {
			return "ALTER COLUMN " + c.Table + "." + c.Column.Name
		}
		return "ALTER COLUMN " + c.Table
	default:
		return "UNKNOWN"
	}
}

// Diff 比對當前 schema 與快照，產生 ChangeSet
func Diff(current []TableSchema, snapshot *Snapshot) []Change {
	var changes []Change

	// 建立當前表的索引
	currentMap := make(map[string]TableSchema)
	for _, t := range current {
		currentMap[t.Name] = t
	}

	// 1. 檢查新增/修改的表
	for _, table := range current {
		old, exists := snapshot.Tables[table.Name]
		if !exists {
			// 新增整張表
			changes = append(changes, Change{
				Type:  AddTable,
				Table: table.Name,
			})
			// 也記錄每個欄位為 AddColumn（用於 SQL 生成）
			for i := range table.Columns {
				col := table.Columns[i]
				changes = append(changes, Change{
					Type:   AddColumn,
					Table:  table.Name,
					Column: &col,
				})
			}
			continue
		}

		// 表存在 → 比對欄位
		changes = append(changes, diffColumns(table.Name, old.Columns, table.Columns)...)
	}

	// 2. 檢查被刪除的表
	for name := range snapshot.Tables {
		if _, exists := currentMap[name]; !exists {
			changes = append(changes, Change{
				Type:  DropTable,
				Table: name,
			})
		}
	}

	return changes
}

// diffColumns 比對同一張表的新舊欄位
func diffColumns(table string, oldCols, newCols []ColumnSchema) []Change {
	var changes []Change

	oldMap := make(map[string]ColumnSchema)
	for _, c := range oldCols {
		oldMap[c.Name] = c
	}

	newMap := make(map[string]ColumnSchema)
	for _, c := range newCols {
		newMap[c.Name] = c
	}

	// 新增或修改的欄位
	for _, col := range newCols {
		old, exists := oldMap[col.Name]
		if !exists {
			c := col
			changes = append(changes, Change{
				Type:   AddColumn,
				Table:  table,
				Column: &c,
			})
			continue
		}

		// 欄位存在 → 檢查是否有變更
		if columnChanged(old, col) {
			newCol := col
			oldCol := old
			changes = append(changes, Change{
				Type:   AlterColumn,
				Table:  table,
				Column: &newCol,
				OldCol: &oldCol,
			})
		}
	}

	// 刪除的欄位
	for _, col := range oldCols {
		if _, exists := newMap[col.Name]; !exists {
			c := col
			changes = append(changes, Change{
				Type:   DropColumn,
				Table:  table,
				Column: &c,
			})
		}
	}

	return changes
}

// columnChanged 判斷欄位是否有變更
func columnChanged(old, new ColumnSchema) bool {
	if old.GoType != new.GoType {
		return true
	}
	if old.SQLType != new.SQLType {
		return true
	}
	if old.NotNull != new.NotNull {
		return true
	}
	if old.PrimaryKey != new.PrimaryKey {
		return true
	}
	if old.AutoIncrement != new.AutoIncrement {
		return true
	}
	if old.Default != new.Default {
		return true
	}
	if old.Unique != new.Unique {
		return true
	}
	return false
}
