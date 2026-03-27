package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Snapshot 儲存上次掃描的 schema 狀態
type Snapshot struct {
	Tables map[string]TableSchema `json:"tables"`
}

// LoadSnapshot 從檔案載入快照
// 若檔案不存在，回傳空快照（首次 diff）
func LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Snapshot{Tables: make(map[string]TableSchema)}, nil
		}
		return nil, fmt.Errorf("failed to read snapshot: %w", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot: %w", err)
	}

	if snap.Tables == nil {
		snap.Tables = make(map[string]TableSchema)
	}
	return &snap, nil
}

// SaveSnapshot 儲存快照到檔案
func SaveSnapshot(path string, tables []TableSchema) error {
	snap := Snapshot{
		Tables: make(map[string]TableSchema, len(tables)),
	}
	for _, t := range tables {
		snap.Tables[t.Name] = t
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}
