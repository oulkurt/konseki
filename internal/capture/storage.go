/*
 * [INPUT]: 依赖采集目录中的日期分区和截图。
 * [OUTPUT]: 提供 Storage、过期清理与存储扩展契约。
 * [POS]: 采集数据的保留策略边界，不删除摘要和设置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrNotImplemented = errors.New("capture: not implemented")

type Cleared struct {
	Sessions, Shots int
	Freed           int64
	Failed          int
}
type Storage struct{ root string }

func NewStorage(root string) *Storage                    { return &Storage{root: root} }
func (s *Storage) Usage() Usage                          { return Usage{} }
func (s *Storage) Account(delta int64)                   {}
func (s *Storage) Enforce(maxBytes int64) (int64, error) { return 0, ErrNotImplemented }
func (s *Storage) Degrade(now time.Time) (int, error)    { return 0, ErrNotImplemented }
func (s *Storage) ClearAll() (Cleared, error)            { return Cleared{}, ErrNotImplemented }
func (s *Storage) Prune(keepDays int, now time.Time) (int, error) {
	if keepDays <= 0 {
		return 0, nil
	}
	cutoff := now.In(time.Local).AddDate(0, 0, -keepDays).Format("2006-01-02")
	removed := map[string]bool{}
	for _, dir := range []string{"", "ocr", "sessions", "shots"} {
		path := filepath.Join(s.root, dir)
		ents, err := os.ReadDir(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return len(removed), err
		}
		for _, e := range ents {
			name := e.Name()
			day := ""
			if dir == "shots" {
				if e.IsDir() {
					day = name
				} else if len(name) > 9 && name[8] == '-' && strings.HasSuffix(name, ".jpg") {
					day = name[:4] + "-" + name[4:6] + "-" + name[6:8]
				}
			} else if !e.IsDir() && strings.HasSuffix(name, ".jsonl") {
				day = strings.TrimSuffix(name, ".jsonl")
			}
			if len(day) != 10 || day >= cutoff {
				continue
			}
			if _, err := time.Parse("2006-01-02", day); err != nil {
				continue
			}
			if err := os.RemoveAll(filepath.Join(path, name)); err != nil {
				return len(removed), err
			}
			removed[day] = true
		}
	}
	return len(removed), nil
}
