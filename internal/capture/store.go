package capture

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Snapshot 是一条最小的事实记录。截图只是证据，不是数据本体——
// 这样后面换任何 UI 或把图删掉都不影响历史。
type Snapshot struct {
	TS      string `json:"ts"`
	App     string `json:"app"`
	Title   string `json:"title"`
	PID     int    `json:"pid"`
	OCR     string `json:"ocr,omitempty"`
	OCRLine int    `json:"ocrLines"`
	Shot    string `json:"shot,omitempty"`
	ImgKB   int    `json:"imgKB"`
	OCRMs   int    `json:"ocrMs"`
}

type Store struct {
	root string
}

func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(root, "shots"), 0o755); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

func (s *Store) dayFile(t time.Time) string {
	return filepath.Join(s.root, t.Format("2006-01-02")+".jsonl")
}

// Append 追加一行 JSONL。只追加不重写，崩溃最多丢最后一条。
func (s *Store) Append(snap Snapshot) error {
	f, err := os.OpenFile(s.dayFile(time.Now()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (s *Store) ShotPath(t time.Time) string {
	return filepath.Join(s.root, "shots", t.Format("20060102-150405")+".jpg")
}

// ReadDay 读回某天的全部记录。
func (s *Store) ReadDay(dateKey string) ([]Snapshot, error) {
	f, err := os.Open(filepath.Join(s.root, dateKey+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var v Snapshot
		if json.Unmarshal(sc.Bytes(), &v) == nil {
			out = append(out, v)
		}
	}
	return out, sc.Err()
}
