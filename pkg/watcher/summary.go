package watcher

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ChangeSummary 描述一批檔案變更
type ChangeSummary struct {
	Timestamp time.Time
	Created   []string
	Modified  []string
	Deleted   []string
}

// BuildSummary 從 fsnotify 事件建立 ChangeSummary
func BuildSummary(events map[string]fsnotify.Op) ChangeSummary {
	s := ChangeSummary{
		Timestamp: time.Now(),
	}

	for path, op := range events {
		switch {
		case op&fsnotify.Create != 0:
			s.Created = append(s.Created, path)
		case op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0:
			s.Deleted = append(s.Deleted, path)
		case op&fsnotify.Write != 0:
			s.Modified = append(s.Modified, path)
		}
	}

	sort.Strings(s.Created)
	sort.Strings(s.Modified)
	sort.Strings(s.Deleted)

	return s
}

// Total 回傳總變更數量
func (s ChangeSummary) Total() int {
	return len(s.Created) + len(s.Modified) + len(s.Deleted)
}

// IsEmpty 是否無變更
func (s ChangeSummary) IsEmpty() bool {
	return s.Total() == 0
}

// String 格式化輸出（供 AI 和人閱讀）
func (s ChangeSummary) String() string {
	if s.IsEmpty() {
		return "No changes detected"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Change Summary [%s] ===\n", s.Timestamp.Format("15:04:05")))

	if len(s.Created) > 0 {
		sb.WriteString(fmt.Sprintf("  Created (%d):\n", len(s.Created)))
		for _, f := range s.Created {
			sb.WriteString(fmt.Sprintf("    + %s\n", f))
		}
	}

	if len(s.Modified) > 0 {
		sb.WriteString(fmt.Sprintf("  Modified (%d):\n", len(s.Modified)))
		for _, f := range s.Modified {
			sb.WriteString(fmt.Sprintf("    ~ %s\n", f))
		}
	}

	if len(s.Deleted) > 0 {
		sb.WriteString(fmt.Sprintf("  Deleted (%d):\n", len(s.Deleted)))
		for _, f := range s.Deleted {
			sb.WriteString(fmt.Sprintf("    - %s\n", f))
		}
	}

	sb.WriteString(fmt.Sprintf("  Total: %d changes\n", s.Total()))
	return sb.String()
}

// JSON 結構化輸出（供 AI 程式化解析）
func (s ChangeSummary) JSON() map[string]interface{} {
	return map[string]interface{}{
		"timestamp": s.Timestamp.Format(time.RFC3339),
		"created":   s.Created,
		"modified":  s.Modified,
		"deleted":   s.Deleted,
		"total":     s.Total(),
	}
}
