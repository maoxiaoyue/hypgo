package manifest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// WriteYAML 將 Manifest 以 YAML 格式寫入 writer
func WriteYAML(w io.Writer, m *Manifest) error {
	encoder := yaml.NewEncoder(w)
	defer encoder.Close()
	encoder.SetIndent(2)
	return encoder.Encode(m)
}

// WriteJSON 將 Manifest 以 JSON 格式寫入 writer
func WriteJSON(w io.Writer, m *Manifest) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(m)
}

// SaveToFile 將 Manifest 儲存到檔案
// format: "yaml" 或 "json"
func SaveToFile(path string, m *Manifest, format string) error {
	// 確保目錄存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", path, err)
	}
	defer f.Close()

	switch format {
	case "json":
		return WriteJSON(f, m)
	default:
		return WriteYAML(f, m)
	}
}
