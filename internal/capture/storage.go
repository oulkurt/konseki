/*
 * [INPUT]: 依赖采集目录、JPEG 编解码、图像缩放和截图文件名。
 * [OUTPUT]: 提供用量缓存、配额淘汰、分层重编码、保留和清空。
 * [POS]: 串行化维护采集数据，与 store.go 共用截图路径规则。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"bufio"
	"bytes"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"
)

var ErrNotImplemented = errors.New("capture: not implemented")

type Cleared struct {
	Sessions, Shots int
	Freed           int64
	Failed          int
}

type degradeKey struct {
	path string
	size int64
	tier int
}

type Storage struct {
	root        string
	maintenance sync.Mutex
	mu          sync.Mutex
	usage       Usage
	scanned     bool
	skipDegrade map[degradeKey]struct{}
}

func NewStorage(root string) *Storage { return &Storage{root: root} }

// Usage 返回缓存的用量，界面每帧都可能调用，不能读盘。
func (s *Storage) Usage() Usage { s.mu.Lock(); defer s.mu.Unlock(); return s.usage }

// Rescan 全量扫一遍采集目录，重算 Usage。启动后和维护任务里由采集循环调用。
// 扫描与采集写入交错时，缓存允许短暂偏差，由下次扫描纠正。
func (s *Storage) Rescan() error {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	return s.rescan()
}

func (s *Storage) rescan() error {
	var u Usage
	shots, err := s.shots()
	if err != nil {
		return err
	}
	for _, shot := range shots {
		u.Bytes += shot.size
		u.Shots++
	}
	for _, item := range []struct {
		dir   string
		count *int
	}{{"", &u.Snapshots}, {"sessions", &u.Sessions}} {
		entries, err := os.ReadDir(filepath.Join(s.root, item.dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !dayJSON(e.Name()) {
				continue
			}
			n, err := lineCount(filepath.Join(s.root, item.dir, e.Name()))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			*item.count += n
		}
	}
	s.mu.Lock()
	s.usage = u
	s.scanned = true
	s.mu.Unlock()
	return nil
}

func (s *Storage) Account(delta int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage.Bytes += delta
	if s.usage.Bytes < 0 {
		s.usage.Bytes = 0
	}
	s.usage.Shots++
	if s.usage.Shots < 0 {
		s.usage.Shots = 0
	}
}

func (s *Storage) CountSnapshot() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usage.Snapshots++
}

func (s *Storage) CountSession() { s.mu.Lock(); defer s.mu.Unlock(); s.usage.Sessions++ }

func (s *Storage) Enforce(maxBytes int64) (int64, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	s.mu.Lock()
	scanned, cachedBytes := s.scanned, s.usage.Bytes
	s.mu.Unlock()
	if scanned && cachedBytes <= maxBytes {
		return 0, nil
	}
	shots, err := s.shots()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, shot := range shots {
		total += shot.size
	}
	if total <= maxBytes {
		s.mu.Lock()
		s.usage.Bytes = total
		s.usage.Shots = len(shots)
		s.scanned = true
		s.mu.Unlock()
		return 0, nil
	}
	if !scanned {
		s.mu.Lock()
		s.usage.Bytes = total
		s.usage.Shots = len(shots)
		s.scanned = true
		s.mu.Unlock()
	}
	sort.Slice(shots, func(i, j int) bool { return shots[i].when.Before(shots[j].when) })
	target := (maxBytes / 10 * 9) + (maxBytes % 10 * 9 / 10)
	if maxBytes < 0 {
		target = 0
	}
	var freed int64
	for _, shot := range shots {
		if total <= target {
			break
		}
		err = os.Remove(shot.path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return freed, err
		}
		freed += shot.size
		total -= shot.size
		s.mu.Lock()
		s.usage.Bytes -= shot.size
		if s.usage.Bytes < 0 {
			s.usage.Bytes = 0
		}
		s.usage.Shots--
		if s.usage.Shots < 0 {
			s.usage.Shots = 0
		}
		s.mu.Unlock()
	}
	return freed, nil
}

func (s *Storage) Degrade(now time.Time) (int, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	shots, err := s.shots()
	if err != nil {
		return 0, err
	}
	counts := [2]int{}
	done := 0
	var firstErr error
	for _, shot := range shots {
		age := now.Sub(shot.when)
		tier, w, h, q := 0, 1280, 720, 40
		if age > 7*24*time.Hour {
			tier, w, h, q = 1, 640, 360, 30
		} else if age <= 24*time.Hour {
			continue
		}
		if counts[tier] >= 500 {
			continue
		}
		key := degradeKey{path: shot.path, size: shot.size, tier: tier}
		if _, skip := s.skipDegrade[key]; skip {
			continue
		}
		changed, size, err := resizeShot(shot, w, h, q)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !changed {
			if s.skipDegrade == nil {
				s.skipDegrade = make(map[degradeKey]struct{})
			}
			s.skipDegrade[key] = struct{}{}
			continue
		}
		counts[tier]++
		done++
		s.mu.Lock()
		s.usage.Bytes += size - shot.size
		if s.usage.Bytes < 0 {
			s.usage.Bytes = 0
		}
		s.mu.Unlock()
	}
	return done, firstErr
}

func (s *Storage) ClearAll() (Cleared, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	s.skipDegrade = nil
	if err := s.rescan(); err != nil {
		return Cleared{}, err
	}
	before := s.Usage()
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return Cleared{}, nil
	}
	if err != nil {
		return Cleared{}, err
	}
	var result Cleared
	for _, e := range entries {
		name := e.Name()
		if (!e.IsDir() && dayJSON(name)) || (e.IsDir() && (name == "ocr" || name == "sessions" || name == "shots")) {
			result.Failed += removeEntries(filepath.Join(s.root, name))
		}
	}
	if err := s.rescan(); err != nil {
		return result, err
	}
	after := s.Usage()
	result.Shots = before.Shots - after.Shots
	result.Freed = before.Bytes - after.Bytes
	result.Sessions = before.Sessions - after.Sessions
	return result, nil
}

func removeEntries(path string) int {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		return 1
	}
	failed := 0
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return 1
		}
		for _, entry := range entries {
			failed += removeEntries(filepath.Join(path, entry.Name()))
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		failed++
	}
	return failed
}

func (s *Storage) Prune(keepDays int, now time.Time) (int, error) {
	s.maintenance.Lock()
	defer s.maintenance.Unlock()
	if keepDays <= 0 {
		return 0, nil
	}
	cutoff := now.In(time.Local).AddDate(0, 0, -keepDays).Format("2006-01-02")
	removed := map[string]bool{}
	var opErr error
	for _, dir := range []string{"", "ocr", "sessions", "shots"} {
		path := filepath.Join(s.root, dir)
		ents, err := os.ReadDir(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			opErr = err
			break
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
				opErr = err
				break
			}
			removed[day] = true
		}
		if opErr != nil {
			break
		}
	}
	if scanErr := s.rescan(); opErr == nil {
		opErr = scanErr
	}
	return len(removed), opErr
}

type shotFile struct {
	path string
	when time.Time
	size int64
}

func dayJSON(name string) bool {
	if !strings.HasSuffix(name, ".jsonl") {
		return false
	}
	_, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".jsonl"))
	return err == nil
}

func lineCount(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	n := 0
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			if len(line) > 0 {
				n++
			}
			return n, nil
		}
		if err != nil {
			return n, err
		}
		n++
	}
}

func (s *Storage) shots() ([]shotFile, error) {
	root := filepath.Join(s.root, "shots")
	var out []shotFile
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !strings.HasSuffix(e.Name(), ".jpg") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		parts := strings.Split(rel, string(filepath.Separator))
		var stamp string
		var layout string
		if len(parts) == 1 {
			stamp = strings.TrimSuffix(parts[0], ".jpg")
			layout = "20060102-150405"
		} else if len(parts) == 2 && len(parts[1]) >= 10 {
			stamp = parts[0] + "-" + parts[1][:10]
			layout = "2006-01-02-150405.000"
		} else {
			return nil
		}
		when, err := time.ParseInLocation(layout, stamp, time.Local)
		if err != nil {
			return nil
		}
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		out = append(out, shotFile{path, when, info.Size()})
		return nil
	})
	return out, err
}

var fullDecodeCount atomic.Uint64

func resizeShot(shot shotFile, maxW, maxH, quality int) (bool, int64, error) {
	f, err := os.Open(shot.path)
	if err != nil {
		return false, 0, err
	}
	config, err := jpeg.DecodeConfig(f)
	if err != nil {
		f.Close()
		return false, 0, err
	}
	w, h := config.Width, config.Height
	long, short := w, h
	if long < short {
		long, short = short, long
	}
	if long <= maxW && short <= maxH {
		f.Close()
		return false, shot.size, nil
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return false, 0, err
	}
	fullDecodeCount.Add(1)
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil {
		return false, 0, err
	}
	b := img.Bounds()
	scale := float64(maxW) / float64(long)
	if v := float64(maxH) / float64(short); v < scale {
		scale = v
	}
	nw, nh := int(float64(w)*scale), int(float64(h)*scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	var buf bytes.Buffer
	if err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return false, 0, err
	}
	if int64(buf.Len()) >= shot.size {
		return false, shot.size, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(shot.path), ".degrade-*.tmp")
	if err != nil {
		return false, 0, err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return false, 0, err
	}
	if err = tmp.Close(); err != nil {
		return false, 0, err
	}
	if _, err = os.Stat(shot.path); err != nil {
		return false, 0, err
	}
	if err = os.Rename(tmp.Name(), shot.path); err != nil {
		return false, 0, err
	}
	return true, int64(buf.Len()), nil
}
