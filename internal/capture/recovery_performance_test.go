/*
 * [INPUT]: 依赖合成历史、临时存储与运行时内存统计。
 * [OUTPUT]: 验证恢复扫描成本不随历史长度增长。
 * [POS]: 会话恢复的性能回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 审查用：合成 days 天历史，最新一天有未关闭会话，测 recoverSession 的耗时与分配量。
func recoveryHistory(t *testing.T, days int) (string, time.Time) {
	root := t.TempDir()
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("屏幕上的文字 screen text ", 80) // 约 2.6 KB
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.Local)
	var last time.Time
	for d := 0; d < days; d++ {
		day := start.AddDate(0, 0, d)
		key := day.Format("2006-01-02")
		snap, err := os.Create(filepath.Join(root, key+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "ocr"), 0755); err != nil {
			t.Fatal(err)
		}
		ocr, err := os.Create(filepath.Join(root, "ocr", key+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		sw, ow := bufio.NewWriter(snap), bufio.NewWriter(ocr)
		for i := 0; i < 2000; i++ {
			ts := day.Add(time.Duration(i) * 15 * time.Second)
			id := fmt.Sprintf("%s-%04x", ts.Format("20060102-150405.000"), i)
			session := fmt.Sprintf("s-%d-%d", d, i/200)
			b, _ := json.Marshal(Snapshot{ID: id, TS: ts.Format(time.RFC3339Nano), Session: session, Trigger: TriggerClick, App: "Editor", Bundle: "com.example.editor", Title: "file.go — project", PID: 42, Shot: "shots/x.jpg", W: 2560, H: 1440, Bytes: 400000})
			sw.Write(append(b, '\n'))
			if i%3 == 0 {
				b, _ = json.Marshal(OCRResult{Snap: id, Text: text, Lines: 40, Ms: 900, Status: "ok"})
				ow.Write(append(b, '\n'))
			}
			last = ts
		}
		sw.Flush()
		ow.Flush()
		snap.Close()
		ocr.Close()
	}
	return root, last
}

func TestRecoverSessionBoundedHistory(t *testing.T) {
	var times [2]time.Duration
	var allocs [2]uint64
	for i, days := range []int{1, 30} {
		root, last := recoveryHistory(t, days)
		st, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		if err := recoverSession(st, NewStorage(root), last.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		times[i] = time.Since(start)
		runtime.ReadMemStats(&after)
		allocs[i] = after.TotalAlloc - before.TotalAlloc
		t.Logf("%d 天: %v, 分配 %d 字节", days, times[i], allocs[i])
	}
	if times[1] > 2*times[0] || allocs[1] > 2*allocs[0] {
		t.Fatalf("30 天恢复超过 1 天的两倍：耗时 %v，分配 %v", times, allocs)
	}
}
