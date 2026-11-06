/*
 * [INPUT]: 依赖 Skill 导出格式和摘要支持的日期。
 * [OUTPUT]: 提供 Reconcile，删除失效日记并重写入口和画像。
 * [POS]: 导出边界的清理操作，只删除可识别的 Konseki 日记。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package skill

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Reconcile 重写导出并删除不再有摘要支持的生成日记。
func Reconcile(opts Options, days []DayData) (int, error) {
	dir := opts.Dir
	if dir == "" {
		dir = DefaultDir()
	}
	supported := make(map[string]bool, len(days))
	for _, day := range days {
		if len(day.Entries) > 0 {
			supported[day.Day] = true
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "days"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		day := strings.TrimSuffix(entry.Name(), ext)
		if (ext != ".md" && ext != ".json") || supported[day] {
			continue
		}
		if _, err := time.Parse("2006-01-02", day); err != nil {
			continue
		}
		path := filepath.Join(dir, "days", entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return 0, err
		}
		if !generatedDay(b, day, ext) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return 0, err
		}
	}
	return Export(opts, days)
}

func generatedDay(b []byte, day, ext string) bool {
	if ext == ".md" {
		return strings.HasPrefix(string(b), "# "+day+"\n\n")
	}
	var value struct {
		Day     string          `json:"day"`
		Entries json.RawMessage `json:"entries"`
		Blocks  json.RawMessage `json:"blocks"`
	}
	return json.Unmarshal(b, &value) == nil && value.Day == day && value.Entries != nil && value.Blocks != nil
}
