/*
 * [INPUT]: 依赖临时导出目录和 Skill 对账操作。
 * [OUTPUT]: 验证失效日记清理与用户文件保留。
 * [POS]: 导出边界的数据清除回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package skill

import (
	"os"
	"path/filepath"
	"testing"

	"konseki/internal/summary"
)

func TestReconcileRemovesStaleGeneratedDays(t *testing.T) {
	dir := t.TempDir()
	opts := Options{Dir: dir}
	days := []DayData{{Day: "2026-11-04", Entries: []summary.Entry{{Headline: "private"}}}}
	if _, err := Export(opts, days); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"notes.md": "mine", "2026-11-05.md": "my own notes"} {
		if err := os.WriteFile(filepath.Join(dir, "days", name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Reconcile(opts, nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-11-04.md", "2026-11-04.json"} {
		if _, err := os.Stat(filepath.Join(dir, "days", name)); !os.IsNotExist(err) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"notes.md", "2026-11-05.md"} {
		if _, err := os.Stat(filepath.Join(dir, "days", name)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "KONSEKI.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" {
		t.Fatal("画像未重写")
	}
}
