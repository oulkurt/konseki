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

// Storage 管采集目录的用量和清理。采集循环写入，维护任务清理，两者可能同时运行；
// Storage 只处理已落盘的 .jpg 和按日期命名的 JSONL，不碰 summaries、config 和 secrets。
type Storage struct{ root string }

func NewStorage(root string) *Storage { return &Storage{root: root} }

// Usage 返回缓存的用量，界面每帧都可能调用，不能读盘。
func (s *Storage) Usage() Usage { return Usage{} }

// Rescan 全量扫一遍采集目录，重算 Usage。启动后和维护任务里由采集循环调用。
func (s *Storage) Rescan() error { return ErrNotImplemented }

// Account 在采集写入一张新截图后调用，delta 是新文件的字节数。
// Storage 把它记成多一张截图，不必等下一次 Rescan。
func (s *Storage) Account(delta int64) {}

// CountSession 在采集写入一条会话关闭记录后调用，会话数不必等下一次 Rescan。
func (s *Storage) CountSession() {}

// Enforce 在用量超过 maxBytes 时从最旧的截图删起，删到 maxBytes 的 90% 以内。快照行保留。
func (s *Storage) Enforce(maxBytes int64) (int64, error) { return 0, ErrNotImplemented }

// Degrade 把超过 1 天的截图重编码成 warm，超过 7 天的重编码成 cold。只看文件当前的尺寸和
// 文件名里的时间，重复执行结果不变。
func (s *Storage) Degrade(now time.Time) (int, error) { return 0, ErrNotImplemented }

// ClearAll 删掉全部快照行、OCR、会话和截图。调用方负责先停下采集循环。
func (s *Storage) ClearAll() (Cleared, error) { return Cleared{}, ErrNotImplemented }

// Prune 删掉早于 keepDays 天的整天数据，keepDays 不大于 0 表示永久保留。
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
