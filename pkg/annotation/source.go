package annotation

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// utf8BOM 為 UTF-8 BOM。go/parser 接受位於檔首的 BOM，但 BOM 出現在檔案中間即為非法字元。
var utf8BOM = string(rune(0xFEFF))

// sourceLines 為改寫 Go 原始檔用的行表示：去掉檔首 BOM 後依 "\n" 切行（CRLF 檔的行尾 "\r" 保留在各行內），
// 寫回時再把 BOM 補回檔首。
type sourceLines struct {
	lines []string
	bom   bool
	// eolCR 為檔案使用 CRLF 時的 "\r"：新插入的行要附加它，否則修補後 CRLF 檔會變成混用換行。
	eolCR string
}

// splitSource 把檔案內容拆成 sourceLines；回傳的 src 為去掉 BOM 後的內容（供 parser 使用，行列號與 lines 一致）。
func splitSource(content []byte) (sourceLines, []byte) {
	s := string(content)
	var sl sourceLines
	if strings.HasPrefix(s, utf8BOM) {
		sl.bom = true
		s = s[len(utf8BOM):]
	}
	if strings.Contains(s, "\r\n") {
		sl.eolCR = "\r"
	}
	sl.lines = strings.Split(s, "\n")
	return sl, []byte(s)
}

// bytes 依 lines 組回檔案內容，原本有 BOM 時補回檔首。
func (sl sourceLines) bytes() []byte {
	s := strings.Join(sl.lines, "\n")
	if sl.bom {
		s = utf8BOM + s
	}
	return []byte(s)
}

var (
	backupMu   sync.Mutex
	backedUpFn = map[string]bool{}
)

// backupOnce 在本行程第一次改寫 filename 之前把 content 寫入 <filename>.bak；
// 同一行程（即同一次 hyp 執行）之後對同一檔案的寫入步驟不再覆寫 .bak，
// 確保 .bak 永遠是本次執行開始前的原始內容。
// 先前 Enrich / FixThink / FixFile 每一步都各自覆寫 .bak，跑完兩步後 .bak 已是中間狀態。
func backupOnce(filename string, content []byte) (string, error) {
	backupPath := filename + ".bak"
	key, err := filepath.Abs(filename)
	if err != nil {
		key = filename
	}
	backupMu.Lock()
	defer backupMu.Unlock()
	if backedUpFn[key] {
		return backupPath, nil
	}
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return "", err
	}
	backedUpFn[key] = true
	return backupPath, nil
}
