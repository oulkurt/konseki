/*
 * [INPUT]: 依赖 KONSEKI_QUERY_PERF、临时数据目录和 search 命令。
 * [OUTPUT]: 在 7 天、每天 2000 条 2 KB 屏幕文字的夹具上打印 search 的五轮耗时和中位数。
 * [POS]: 查询命令的性能门槛，未设置环境变量时跳过。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
)

func TestQueryPerf(t *testing.T) {
	if os.Getenv("KONSEKI_QUERY_PERF") == "" {
		t.Skip("KONSEKI_QUERY_PERF 未设置")
	}
	dir := t.TempDir()
	cfg := config.Default()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("activity on screen ", 114)
	for d := 0; d < 7; d++ {
		day := fixtureNow.AddDate(0, 0, -d).Format("2006-01-02")
		f, err := os.Create(filepath.Join(dir, day+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		w := bufio.NewWriter(f)
		enc := json.NewEncoder(w)
		for i := 0; i < 2000; i++ {
			at := fixtureNow.AddDate(0, 0, -d).Add(time.Duration(i) * 4 * time.Second)
			sn := capture.Snapshot{
				ID: fmt.Sprintf("%s-%05d", at.Format("20060102-150405.000"), i),
				TS: at.Format(time.RFC3339), App: "Editor", Title: "main.go",
				OCR: text, Keys: 1,
			}
			if err := enc.Encode(sn); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Flush(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	env := Env{Dir: dir, Now: func() time.Time { return fixtureNow }}
	for _, term := range []string{"needle", "activity"} {
		var rounds []time.Duration
		for i := 0; i < 5; i++ {
			start := time.Now()
			out, stderr, code := runQuery(t, env, "search", term)
			if code != 0 {
				t.Fatalf("第 %d 轮失败: %s", i+1, stderr)
			}
			got := decode[searchOutput](t, out)
			want := 0
			if term == "activity" {
				want = 7
			}
			if got.Total != want {
				t.Fatalf("%s total=%d want=%d", term, got.Total, want)
			}
			if term == "activity" {
				for _, m := range got.Matches {
					if m.Count != 2000 {
						t.Fatalf("count=%d", m.Count)
					}
				}
			}
			d := time.Since(start)
			rounds = append(rounds, d)
			fmt.Printf("round %d: %s\n", i+1, d)
		}
		sorted := append([]time.Duration(nil), rounds...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		median := sorted[len(sorted)/2]
		fmt.Printf("%s median: %s\n", term, median)
		if median > 2*time.Second {
			t.Fatalf("中位数 %s 超过 2 秒", median)
		}
	}

}
