/*
 * [INPUT]: 依赖临时目录、采集存储、摘要存储和设置编码。
 * [OUTPUT]: 提供查询测试用的两天夹具、键鼠计数与命令运行工具。
 * [POS]: query 包测试的数据构造器，全部数据落在 t.TempDir()。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/summary"
)

var fixtureNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)

type testFixture struct {
	t     *testing.T
	dir   string
	store *capture.Store
	sums  *summary.Store
	env   Env
}

func newFixture(t *testing.T, cfg config.Config) *testFixture {
	t.Helper()
	dir := t.TempDir()
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := capture.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := summary.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &testFixture{
		t: t, dir: dir, store: store, sums: sums,
		env: Env{Dir: dir, Now: func() time.Time { return fixtureNow }},
	}
}

func (f *testFixture) addSnapshot(at time.Time, id, app, bundle, title, ocr string, withShot bool) {
	f.t.Helper()
	sn := capture.Snapshot{
		ID: id, TS: at.Format(time.RFC3339), App: app, Bundle: bundle,
		Title: title, OCR: ocr, Trigger: capture.TriggerAppFocus,
		Keys: new(3), Clicks: new(1),
	}
	if withShot {
		path := f.store.ShotFile(id)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("jpeg-bytes"), 0o644); err != nil {
			f.t.Fatal(err)
		}
		rel, err := filepath.Rel(f.dir, path)
		if err != nil {
			f.t.Fatal(err)
		}
		sn.Shot = rel
	}
	if err := f.store.Append(sn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *testFixture) addSession(s capture.Session) {
	f.t.Helper()
	if err := f.store.AppendSession(s); err != nil {
		f.t.Fatal(err)
	}
}

func (f *testFixture) addEntry(e summary.Entry) {
	f.t.Helper()
	if err := f.sums.Upsert(e); err != nil {
		f.t.Fatal(err)
	}
}

// standardFixture 造两天数据：今天 2026-11-05 与昨天 2026-11-04。
func standardFixture(t *testing.T) *testFixture {
	t.Helper()
	f := newFixture(t, config.Default())
	at := func(day, h, m int) time.Time {
		return time.Date(2026, 11, day, h, m, 0, 0, time.Local)
	}

	f.addSnapshot(at(5, 9, 0), "20261105-090000.000-0001", "Editor", "com.example.editor", "main.go — Konseki", "func main() {\n\tfmt.Println(\"hello\")\n}", true)
	f.addSnapshot(at(5, 9, 5), "20261105-090500.000-0002", "Browser", "com.example.browser", "Go documentation", "The Go programming language", false)
	f.addSnapshot(at(5, 10, 0), "20261105-100000.000-0003", "Editor", "com.example.editor", "query.go — Konseki", "The Go programming language", false)
	f.addSnapshot(at(5, 11, 0), "20261105-110000.000-0004", "Slack", "com.tinyspeck.slackmacgap", "team chat", "alpha beta gamma", false)
	f.addSnapshot(at(4, 14, 0), "20261104-140000.000-0005", "Terminal", "com.apple.Terminal", "notes.md", "alpha delta", false)

	f.addSession(capture.Session{
		ID: "session-today", Start: at(5, 9, 0), End: at(5, 11, 20),
		EndReason: capture.EndIdle, Apps: []string{"Editor", "Browser", "Slack"},
		Snapshots: 4, Keys: new(12), Clicks: new(3),
	})
	f.addSession(capture.Session{
		ID: "session-yesterday", Start: at(4, 14, 0), End: at(4, 15, 0),
		EndReason: capture.EndStop, Apps: []string{"Terminal"}, Snapshots: 1,
	})

	f.addEntry(summary.Entry{
		ID: "2026-11-05-aaaa", Day: "2026-11-05", Start: at(5, 9, 0), End: at(5, 10, 0),
		Headline: "编写查询命令", Summary: "实现只读命令。", Apps: []string{"Editor"},
		Topics: []string{"query"}, Open: []string{"测试未跑"}, Decisions: []string{"输出 JSON"},
		People: []string{"Linus"}, Files: []string{"internal/query/query.go"},
	})
	f.addEntry(summary.Entry{
		ID: "2026-11-05-bbbb", Day: "2026-11-05", Start: at(5, 10, 0), End: at(5, 11, 0),
		Headline: "查文档", Summary: "读 Go 文档。", Apps: []string{"Browser"},
	})
	f.addEntry(summary.Entry{
		ID: "2026-11-04-cccc", Day: "2026-11-04", Start: at(4, 14, 0), End: at(4, 15, 0),
		Headline: "记笔记", Summary: "整理 alpha 记录。", Apps: []string{"Terminal"},
	})
	return f
}

func runQuery(t *testing.T, env Env, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, env, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func decode[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("输出不是合法 JSON: %v\n%s", err, out)
	}
	return v
}

func defaultConfig() config.Config { return config.Default() }
