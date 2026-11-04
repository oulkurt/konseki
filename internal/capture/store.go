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

// OCRIndex 把某天的 OCR 文本按窗口标题收拢，给摘要用。
//
// 同一个标题会出现很多次（一个文件开着半小时），只留最长的那份 ——
// OCR 长的那份通常信息更多（加载完了、滚动到了正文）。这里不做去重
// 或清洗，摘要把关那一步自己会截断。
func (s *Store) OCRIndex(dateKey string) (map[string]string, error) {
	snaps, err := s.ReadDay(dateKey)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(snaps))
	for _, sn := range snaps {
		if sn.Title == "" || len(sn.OCR) < 40 {
			continue
		}
		if len(sn.OCR) > len(out[sn.Title]) {
			out[sn.Title] = sn.OCR
		}
	}
	return out, nil
}
