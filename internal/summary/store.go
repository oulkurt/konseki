/*
 * [INPUT]: 依赖摘要条目与跨平台文件锁。
 * [OUTPUT]: 提供带代次隔离、严格读检验与只读保护的 JSONL 摘要存储。
 * [POS]: 摘要模块的持久化边界；跨写入者串行化读-改-写。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package summary

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const genFile = ".gen"

var (
	ErrReadOnly   = errors.New("summary: read-only store")
	ErrStaleWrite = errors.New("summary: stale write")
)

type Store struct {
	dir      string
	readOnly bool
	mu       sync.Mutex
	gen      uint64
}

func NewStore(root string) (*Store, error) {
	dir := filepath.Join(root, "summaries")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func OpenStore(root string) *Store {
	return &Store{dir: filepath.Join(root, "summaries"), readOnly: true}
}

func validDay(day string) bool {
	if len(day) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", day)
	return err == nil
}

func (s *Store) dayPath(day string) string {
	return filepath.Join(s.dir, day+".jsonl")
}

func readStoreGenLocked(dir string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(dir, genFile))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	raw := strings.TrimSpace(string(b))
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}

func writeStoreGenLocked(dir string, gen uint64) error {
	f, err := os.CreateTemp(dir, ".gen.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(strconv.FormatUint(gen, 10) + "\n"); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, genFile)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) Generation() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly {
		if diskGen, err := readStoreGenLocked(s.dir); err == nil && diskGen > s.gen {
			return diskGen
		}
		return s.gen
	}
	unlock, err := lockStoreFile(s.dir)
	if err != nil {
		return s.gen
	}
	defer unlock()
	if diskGen, err := readStoreGenLocked(s.dir); err == nil && diskGen > s.gen {
		s.gen = diskGen
	}
	return s.gen
}

func (s *Store) Load(day string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(day, false)
}

func (s *Store) loadLocked(day string, strict bool) ([]Entry, error) {
	b, err := os.ReadFile(s.dayPath(day))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byID := map[string]Entry{}
	var order []string
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			if strict {
				return nil, fmt.Errorf("summary: corrupt entry in %s.jsonl line %d: %w", day, i+1, err)
			}
			continue
		}
		if strings.TrimSpace(e.ID) == "" {
			if strict {
				return nil, fmt.Errorf("summary: empty entry ID in %s.jsonl line %d", day, i+1)
			}
			continue
		}
		if _, seen := byID[e.ID]; !seen {
			order = append(order, e.ID)
		}
		byID[e.ID] = e
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func (s *Store) Upsert(e Entry) error {
	return s.upsert(e, false, 0, nil)
}

func (s *Store) UpsertIfGen(e Entry, expectedGen uint64) error {
	return s.upsert(e, true, expectedGen, nil)
}

func (s *Store) upsertGuarded(e Entry, expectedGen uint64, commitGuard func(func() error) error) error {
	return s.upsert(e, true, expectedGen, commitGuard)
}

func (s *Store) upsert(e Entry, checkGen bool, expectedGen uint64, commitGuard func(func() error) error) error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("summary: empty entry ID")
	}
	if !validDay(e.Day) {
		return fmt.Errorf("summary: invalid day %q", e.Day)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly {
		return ErrReadOnly
	}
	unlock, err := lockStoreFile(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	if checkGen {
		diskGen, err := readStoreGenLocked(s.dir)
		if err != nil {
			return err
		}
		if diskGen > s.gen {
			s.gen = diskGen
		}
		if s.gen != expectedGen {
			return ErrStaleWrite
		}
	}
	existing, err := s.loadLocked(e.Day, true)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	replaced := false
	for i := range existing {
		if existing[i].ID == e.ID {
			existing[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		existing = append(existing, e)
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].Start.Before(existing[j].Start) })

	var sb strings.Builder
	for _, x := range existing {
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	f, err := os.CreateTemp(s.dir, e.Day+".jsonl.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.WriteString(sb.String()); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	doRename := func() error {
		return os.Rename(tmp, s.dayPath(e.Day))
	}
	if commitGuard != nil {
		err = commitGuard(doRename)
	} else {
		err = doRename()
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) Days() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(n, ".jsonl")
		if !validDay(day) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, day)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func isSummaryDayArtifact(name string) bool {
	if strings.HasSuffix(name, ".jsonl") {
		return validDay(strings.TrimSuffix(name, ".jsonl"))
	}
	if !strings.HasSuffix(name, ".tmp") {
		return false
	}
	stem := strings.TrimSuffix(name, ".tmp")
	if strings.HasSuffix(stem, ".jsonl") {
		return validDay(strings.TrimSuffix(stem, ".jsonl"))
	}
	if i := strings.Index(stem, ".jsonl."); i > 0 {
		return validDay(stem[:i])
	}
	return false
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readOnly {
		return ErrReadOnly
	}
	unlock, err := lockStoreFile(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	diskGen, err := readStoreGenLocked(s.dir)
	if err != nil {
		return err
	}
	nextGen := max(s.gen, diskGen) + 1
	if err := writeStoreGenLocked(s.dir, nextGen); err != nil {
		return err
	}
	s.gen = nextGen
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isSummaryDayArtifact(name) {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
			return err
		}
	}
	return nil
}
