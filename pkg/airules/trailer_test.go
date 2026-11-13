package airules

import (
	"strings"
	"testing"

	"github.com/maoxiaoyue/hypgo/pkg/manifest"
)

// hash 在檔尾：路由表變動時，開頭到第一處實際變動之前要完全相同（prompt cache 可命中）。
// 歷史 bug：hash 與標記同在第一行，任何變動都讓檔案從第 43 個字元起不同
func TestHashAtEndKeepsPrefixStable(t *testing.T) {
	m1 := &manifest.Manifest{Routes: []manifest.RouteManifest{{Method: "GET", Path: "/a", Summary: "A"}}}
	m2 := &manifest.Manifest{Routes: []manifest.RouteManifest{{Method: "GET", Path: "/a", Summary: "A"}, {Method: "POST", Path: "/b", Summary: "B"}}}
	for _, target := range AllTargets() {
		a, b := target.Generate(m1, Options{}), target.Generate(m2, Options{})
		if a == b {
			continue // 沒有路由表的目標（如超過上限的 Windsurf）
		}
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		table := strings.Index(a, "Current Routes")
		if table < 0 || i < table {
			t.Errorf("%s: files diverge at byte %d, before the route table at %d", target.Name, i, table)
		}
		if lines := strings.Split(strings.TrimRight(a, "\n"), "\n"); !trailerLine.MatchString(lines[len(lines)-1]) {
			t.Errorf("%s: last line should carry the hash: %q", target.Name, lines[len(lines)-1])
		}
		if strings.Contains(strings.SplitN(a, "Current Routes", 2)[0], "sha256:") {
			t.Errorf("%s: hash appears before the route table", target.Name)
		}
		if classify([]byte(a)) != fileGenerated {
			t.Errorf("%s: fresh output not recognised as generated", target.Name)
		}
	}
}

func TestClassifyTrailer(t *testing.T) {
	gen := render("", "# Title\n\nbody\n")
	genFM := render("---\ntrigger: always_on\n---\n", "\n# Title\n")
	trailer := gen[strings.LastIndex(gen, trailerPrefix):]
	// v0.9.3 開發期間的格式：hash 在開頭
	oldStyle := markerPrefix + " sha256:" + contentHash("# Title\n\nbody\n") + " -->\n# Title\n\nbody\n"

	cases := map[string]struct {
		data string
		want fileKind
	}{
		"generated":                {gen, fileGenerated},
		"generated + frontmatter":  {genFM, fileGenerated},
		"crlf":                     {strings.ReplaceAll(gen, "\n", "\r\n"), fileGenerated},
		"bom":                      {"\xef\xbb\xbf" + gen, fileGenerated},
		"extra final newlines":     {gen + "\n\n", fileGenerated},
		"no final newline":         {strings.TrimRight(gen, "\n"), fileGenerated},
		"body edited":              {strings.Replace(gen, "body", "BODY", 1), fileModified},
		"appended after the hash":  {gen + "my notes\n", fileModified},
		"inserted before the hash": {strings.Replace(gen, trailer, "my notes\n"+trailer, 1), fileModified},
		"frontmatter edited":       {strings.Replace(genFM, "always_on", "manual", 1), fileModified},
		"hash line removed":        {strings.Replace(gen, trailer, "", 1), fileLegacy},
		"hash at the top (old)":    {oldStyle, fileGenerated},
		"old, edited":              {oldStyle + "x\n", fileModified},
		"trailer but no marker":    {"# Mine\n\n" + trailer, fileManual},
	}
	for name, c := range cases {
		if got := classify([]byte(c.data)); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
}
