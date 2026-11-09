/*
 * [INPUT]: 依赖采集根目录中的快照、OCR 和会话 JSONL。
 * [OUTPUT]: 追加并读取快照、OCR 和会话；保留键鼠计数缺失与零值；只读打开、列出快照与会话日期并集、解析时间、还原截图路径。
 * [POS]: 采集事实的追加存储，兼容旧快照格式。只读接口不建目录、不写文件。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Trigger string

const (
	TriggerAppFocus    Trigger = "app_focus"
	TriggerClick       Trigger = "click"
	TriggerTypingPause Trigger = "typing_pause"
	TriggerVisual      Trigger = "visual_change"
	TriggerHeartbeat   Trigger = "heartbeat"
)

type Snapshot struct {
	ID      string  `json:"id,omitempty"`
	TS      string  `json:"ts"`
	Session string  `json:"session,omitempty"`
	Trigger Trigger `json:"trigger,omitempty"`
	App     string  `json:"app"`
	Bundle  string  `json:"bundle,omitempty"`
	Title   string  `json:"title"`
	PID     int     `json:"pid"`
	Shot    string  `json:"shot,omitempty"`
	Dup     bool    `json:"dup,omitempty"`
	W       int     `json:"w,omitempty"`
	H       int     `json:"h,omitempty"`
	Bytes   int64   `json:"bytes,omitempty"`
	// Keys、Clicks 为 nil 表示没计，指向 0 表示计了没按。
	Keys    *int   `json:"keys,omitempty"`
	Clicks  *int   `json:"clicks,omitempty"`
	OCR     string `json:"ocr,omitempty"`
	OCRLine int    `json:"ocrLines"`
	ImgKB   int    `json:"imgKB"`
	OCRMs   int    `json:"ocrMs"`
}

type OCRResult struct {
	Snap   string `json:"snap"`
	Text   string `json:"text"`
	Lines  int    `json:"lines"`
	Ms     int    `json:"ms"`
	Status string `json:"status"`
}

type Store struct{ root string }

func NewStore(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(abs, "shots"), 0755); err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}

// OpenStore 只读打开采集目录，不建目录、不写文件。目录不存在时返回的 Store 读出来是空的。
func OpenStore(root string) (*Store, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}

// Days 合并快照和会话文件的日期，最新的在前。
func (s *Store) Days() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sessionEntries, err := os.ReadDir(filepath.Join(s.root, "sessions"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	entries = append(entries, sessionEntries...)
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !dayJSON(e.Name()) || seen[e.Name()] {
			continue
		}
		seen[e.Name()] = true
		out = append(out, strings.TrimSuffix(e.Name(), ".jsonl"))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

func (s *Store) dayFile(t time.Time) string {
	return filepath.Join(s.root, t.Format("2006-01-02")+".jsonl")
}

func appendJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func readJSON[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var v T
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			out = append(out, v)
		}
	}
	return out, sc.Err()
}

func (s *Store) Append(v Snapshot) error {
	t, err := parseTS(v.TS)
	if err != nil {
		t = time.Now()
	}
	return appendJSON(s.dayFile(t.In(time.Local)), v)
}
func (s *Store) ShotPath(t time.Time) string {
	return filepath.Join(s.root, "shots", t.Format("20060102-150405")+".jpg")
}

var shotIDs struct {
	sync.Mutex
	timestamp string
	used      map[uint16]struct{}
}

// NewSnapshotID 生成形如 20261105-143502.120-1a2b 的快照 id，同一进程内同一毫秒不重复。
// 截图路径由 ShotFile 从 id 推出，两者同源。
func (s *Store) NewSnapshotID(t time.Time) string {
	stamp := t.Format("20060102-150405.000")
	shotIDs.Lock()
	defer shotIDs.Unlock()
	if shotIDs.timestamp != stamp {
		shotIDs.timestamp = stamp
		shotIDs.used = make(map[uint16]struct{})
	}
	if len(shotIDs.used) == 1<<16 {
		panic("capture: snapshot ID space exhausted")
	}
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		value := binary.BigEndian.Uint16(b[:])
		if _, exists := shotIDs.used[value]; !exists {
			shotIDs.used[value] = struct{}{}
			return fmt.Sprintf("%s-%04x", stamp, value)
		}
	}
}

// ShotFile 返回快照 id 对应的截图路径。文件名取自 id，日期目录取自 id 的日期段，
// 拿到任何一张截图都能反查到它的快照行。
func (s *Store) ShotFile(id string) string {
	day := id[:4] + "-" + id[4:6] + "-" + id[6:8]
	return filepath.Join(s.root, "shots", day, id[9:]+".jpg")
}

func (s *Store) AppendOCR(day string, r OCRResult) error {
	return appendJSON(filepath.Join(s.root, "ocr", day+".jsonl"), r)
}

func (s *Store) ReadOCR(day string) ([]OCRResult, error) {
	return readJSON[OCRResult](filepath.Join(s.root, "ocr", day+".jsonl"))
}

func (s *Store) AppendSession(v Session) error {
	return appendJSON(filepath.Join(s.root, "sessions", v.End.In(time.Local).Format("2006-01-02")+".jsonl"), v)
}

func (s *Store) ReadSessions(day string) ([]Session, error) {
	return readJSON[Session](filepath.Join(s.root, "sessions", day+".jsonl"))
}

// ReadSnapshots 只读快照行，不回填 OCR。需要屏幕文字时用 ReadDay。
func (s *Store) ReadSnapshots(day string) ([]Snapshot, error) {
	return readJSON[Snapshot](filepath.Join(s.root, day+".jsonl"))
}

// Time 解析快照的时间。时间格式由采集写入，解析失败说明这一行损坏。
func (s Snapshot) Time() (time.Time, error) {
	return parseTS(s.TS)
}

// ShotOf 返回截图文件的绝对路径。没写截图时返回空。
// 写盘时存的是相对采集根目录的路径，见 runner.go 的 persist。
func (s *Store) ShotOf(sn Snapshot) string {
	if sn.Shot == "" {
		return ""
	}
	if filepath.IsAbs(sn.Shot) {
		return sn.Shot
	}
	return filepath.Join(s.root, sn.Shot)
}

func (s *Store) ReadDay(day string) ([]Snapshot, error) {
	out, err := s.ReadSnapshots(day)
	if err != nil {
		return nil, err
	}
	ocr, err := s.ReadOCR(day)
	if err != nil {
		return nil, err
	}
	index := map[string]OCRResult{}
	for _, r := range ocr {
		if r.Status == "ok" && r.Snap != "" {
			index[r.Snap] = r
		}
	}
	for i := range out {
		if out[i].OCR == "" && out[i].ID != "" {
			if r, ok := index[out[i].ID]; ok {
				out[i].OCR = r.Text
				out[i].OCRLine = r.Lines
			}
		}
	}
	return out, nil
}

func (s *Store) OCRIndex(day string) (map[string]string, error) {
	snaps, err := s.ReadDay(day)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(snaps))
	for _, sn := range snaps {
		if sn.Title != "" && len(sn.OCR) >= 40 && len(sn.OCR) > len(out[sn.Title]) {
			out[sn.Title] = sn.OCR
		}
	}
	return out, nil
}
