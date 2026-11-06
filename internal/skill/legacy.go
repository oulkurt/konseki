/*
 * [INPUT]: 依赖文件系统与旧导出器写出的文件内容。
 * [OUTPUT]: 提供 LegacyDir、FindLegacy 和 RemoveLegacy。
 * [POS]: 只识别和删除旧版技能导出的文件；新技能在仓库 skills/konseki，数据经 internal/query 读取。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package skill 只识别和删除旧版技能导出写出的文件。
//
// 新技能位于仓库的 skills/konseki，由 skills 命令行复制到用户机器，
// 数据经 internal/query 的只读命令读取。旧导出器写出的静态快照不再更新，
// 还占着新技能的安装位置，所以这里只保留识别和删除。
package skill

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 以下字面量来自 99394ed 的旧导出器。只有同时命中这些标记才当成旧文件，
// 避免误删用户自己的 SKILL.md 或日记。
const (
	legacySkillPrefix      = "---\nname: konseki\n"
	legacySkillTitle       = "# 痕跡 Konseki — "
	legacySkillUpdate      = "Konseki 每次生成新摘要后会重写这个目录。"
	legacyProfilePrefix    = "# Konseki 工作画像\n\n"
	legacyDaySummary       = "## 摘要\n\n"
	legacyDayTimeline      = "## 原始时间轴\n\n"
	legacyDayEmpty         = "这一天没有记录。\n"
	legacySkillFile        = "SKILL.md"
	legacyProfileFile      = "KONSEKI.md"
	legacyDaysDir          = "days"
	legacyDayLayout        = "2006-01-02"
	legacySkillDefaultName = "konseki"
)

// LegacyDir 返回旧导出器的默认目录，也是新技能的安装位置。
func LegacyDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return legacySkillDefaultName
	}
	return filepath.Join(home, ".claude", "skills", legacySkillDefaultName)
}

// FindLegacy 返回 dir 里旧导出器写出的文件。目录不存在时返回空。
func FindLegacy(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		switch e.Name() {
		case legacySkillFile:
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			text := string(b)
			if strings.HasPrefix(text, legacySkillPrefix) &&
				strings.Contains(text, legacySkillTitle) &&
				strings.Contains(text, legacySkillUpdate) {
				out = append(out, path)
			}
		case legacyProfileFile:
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(string(b), legacyProfilePrefix) {
				out = append(out, path)
			}
		}
	}

	days, err := os.ReadDir(filepath.Join(dir, legacyDaysDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range days {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		day := strings.TrimSuffix(name, ext)
		if ext != ".md" && ext != ".json" {
			continue
		}
		if _, err := time.Parse(legacyDayLayout, day); err != nil {
			continue
		}
		path := filepath.Join(dir, legacyDaysDir, name)
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if generatedDay(b, day, ext) {
			out = append(out, path)
		}
	}
	return out, nil
}

// RemoveLegacy 删除 dir 里的旧导出文件，返回删除的文件数。
//
// days/ 空了就删，dir 空了也删。dir 本身是软链接时只删里面的文件，
// 不删链接和链接目标目录。
func RemoveLegacy(dir string) (int, error) {
	files, err := FindLegacy(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, path := range files {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return n, err
		}
		n++
	}
	removeEmptyDir(filepath.Join(dir, legacyDaysDir))
	removeEmptyDir(dir)
	return n, nil
}

// removeEmptyDir 只删除空的真实目录。软链接和文件不动。
func removeEmptyDir(path string) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return
	}
	_ = os.Remove(path)
}

// generatedDay 判断一个日记文件是否由旧导出器生成。
func generatedDay(b []byte, day, ext string) bool {
	if ext == ".md" {
		text := string(b)
		return strings.HasPrefix(text, "# "+day+"\n\n") &&
			(strings.Contains(text, legacyDaySummary) ||
				strings.Contains(text, legacyDayTimeline) ||
				strings.HasSuffix(text, legacyDayEmpty))
	}
	var value struct {
		Day     string          `json:"day"`
		Entries json.RawMessage `json:"entries"`
		Blocks  json.RawMessage `json:"blocks"`
	}
	return json.Unmarshal(b, &value) == nil && value.Day == day && value.Entries != nil && value.Blocks != nil
}
