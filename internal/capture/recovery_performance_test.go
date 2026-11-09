/*
 * [INPUT]: 依赖合成历史、临时存储与运行时内存统计。
 * [OUTPUT]: 验证恢复扫描的分配量不随历史长度增长；耗时只记日志，不做断言。
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

// recoveryHistory 合成 days 天历史。每天 2000 张快照、每 3 张一条约 2.6 KB 的 OCR，
// 最新一天的最后一个会话没有关闭记录，供恢复去找。
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
	// 只断言分配量。30 天要比 1 天多扫一天才能确认会话到头，耗时比值本来就在 1.2 到 2 倍之间，
	// -race 加负载时会越过 2 倍误报。扫完全部历史时分配量是 1 天的几十倍，不会漏掉。
	if allocs[1] > 2*allocs[0] {
		t.Fatalf("30 天恢复的分配量超过 1 天的两倍：%v（耗时 %v）", allocs, times)
	}
}
