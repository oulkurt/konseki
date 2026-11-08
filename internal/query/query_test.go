/*
 * [INPUT]: 依赖两天夹具和命令入口。
 * [OUTPUT]: 验证 status、days、day、snapshots 的输出、用法错误、只读性和软链接。
 * [POS]: query 包命令契约与文件系统边界的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"konseki/internal/capture"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStatusOutput(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "status")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	var got struct {
		DataDir    string `json:"dataDir"`
		CaptureDir string `json:"captureDir"`
		Recorder   struct {
			Enabled     bool   `json:"enabled"`
			PausedUntil string `json:"pausedUntil"`
		} `json:"recorder"`
		Language  string `json:"language"`
		Redaction struct {
			Enabled  bool `json:"enabled"`
			Secrets  bool `json:"secrets"`
			Personal bool `json:"personal"`
		} `json:"redaction"`
		Days struct {
			Count int    `json:"count"`
			First string `json:"first"`
			Last  string `json:"last"`
		} `json:"days"`
		Today struct {
			Day       string `json:"day"`
			Snapshots int    `json:"snapshots"`
			Summaries int    `json:"summaries"`
		} `json:"today"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.DataDir != f.dir || got.CaptureDir != f.dir {
		t.Fatalf("目录 = %q / %q", got.DataDir, got.CaptureDir)
	}
	if !got.Recorder.Enabled || got.Recorder.PausedUntil != "" {
		t.Fatalf("记录器 = %+v", got.Recorder)
	}
	if got.Language != "zh-Hans" {
		t.Fatalf("语言 = %q", got.Language)
	}
	if !got.Redaction.Enabled || !got.Redaction.Secrets || !got.Redaction.Personal {
		t.Fatalf("脱敏 = %+v", got.Redaction)
	}
	if got.Days.Count != 2 || got.Days.First != "2026-11-04" || got.Days.Last != "2026-11-05" {
		t.Fatalf("日期 = %+v", got.Days)
	}
	if got.Today.Day != "2026-11-05" || got.Today.Snapshots != 4 || got.Today.Summaries != 2 {
		t.Fatalf("今天 = %+v", got.Today)
	}
	if strings.Contains(out, "words") || strings.Contains(out, "allow") {
		t.Fatalf("status 输出了脱敏词表或放行表: %s", out)
	}
}

func TestDaysOutput(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "days", "--limit", "1")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	got := decode[struct {
		Days []dayCount `json:"days"`
	}](t, out)
	if len(got.Days) != 1 {
		t.Fatalf("天数 = %d", len(got.Days))
	}
	d := got.Days[0]
	if d.Day != "2026-11-05" || d.Snapshots != 4 || d.Summaries != 2 || d.Sessions != 1 {
		t.Fatalf("第一天 = %+v", d)
	}

	out, _, _ = runQuery(t, f.env, "days")
	got = decode[struct {
		Days []dayCount `json:"days"`
	}](t, out)
	if len(got.Days) != 2 || got.Days[0].Day != "2026-11-05" || got.Days[1].Day != "2026-11-04" {
		t.Fatalf("顺序 = %+v", got.Days)
	}
	if got.Days[1].Snapshots != 1 || got.Days[1].Summaries != 1 || got.Days[1].Sessions != 1 {
		t.Fatalf("第二天 = %+v", got.Days[1])
	}
}

func TestDayOutput(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "day")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	got := decode[struct {
		Day       string          `json:"day"`
		Summaries []entryOutput   `json:"summaries"`
		Blocks    []blockOutput   `json:"blocks"`
		Sessions  []sessionOutput `json:"sessions"`
	}](t, out)
	if got.Day != "2026-11-05" || len(got.Summaries) != 2 || len(got.Blocks) != 4 || len(got.Sessions) != 1 {
		t.Fatalf("day = %+v", got)
	}
	e := got.Summaries[0]
	if e.ID != "2026-11-05-aaaa" || e.Headline != "编写查询命令" || len(e.Open) != 1 || e.Open[0] != "测试未跑" {
		t.Fatalf("摘要 = %+v", e)
	}
	if _, err := time.Parse(time.RFC3339, e.Start); err != nil {
		t.Fatalf("时间格式 = %q", e.Start)
	}
	if got.Blocks[0].App != "Editor" || got.Blocks[0].Minutes != 0 || got.Blocks[0].Samples != 1 || len(got.Blocks[0].Titles) != 1 {
		t.Fatalf("第一块 = %+v", got.Blocks[0])
	}
	if got.Blocks[1].App != "Browser" || got.Blocks[2].App != "Editor" || got.Blocks[3].App != "Slack" {
		t.Fatalf("块顺序 = %+v", got.Blocks)
	}
	s := got.Sessions[0]
	if s.ID != "session-today" || s.EndReason != "idle" || len(s.Apps) != 3 || s.Keys != 12 || s.Clicks != 3 {
		t.Fatalf("会话 = %+v", s)
	}

	out, _, _ = runQuery(t, f.env, "day", "--date", "yesterday")
	// 昨天的会话没有键鼠计数：不输出 keys、clicks，输出 0 会被读成「没打字」。
	if strings.Contains(out, `"keys"`) || strings.Contains(out, `"clicks"`) {
		t.Fatalf("没有计数的会话输出了 keys 或 clicks：%s", out)
	}
	yesterday := decode[struct {
		Day       string        `json:"day"`
		Summaries []entryOutput `json:"summaries"`
	}](t, out)
	if yesterday.Day != "2026-11-04" || len(yesterday.Summaries) != 1 {
		t.Fatalf("昨天 = %+v", yesterday)
	}
}

func TestSnapshotsOutput(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "snapshots", "--date", "2026-11-05", "--ocr", "--limit", "3")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	got := decode[struct {
		Day       string           `json:"day"`
		Total     int              `json:"total"`
		Returned  int              `json:"returned"`
		Truncated bool             `json:"truncated"`
		Snapshots []snapshotOutput `json:"snapshots"`
	}](t, out)
	if got.Day != "2026-11-05" || got.Total != 4 || got.Returned != 3 || !got.Truncated {
		t.Fatalf("头部 = %+v", got)
	}
	if got.Snapshots[0].ID != "20261105-090500.000-0002" || got.Snapshots[2].ID != "20261105-110000.000-0004" {
		t.Fatalf("最近 3 条 = %+v", got.Snapshots)
	}
	if got.Snapshots[0].OCR != "The Go programming language" {
		t.Fatalf("OCR = %q", got.Snapshots[0].OCR)
	}
	if !got.Snapshots[1].OCRRepeat || got.Snapshots[1].OCR != "" {
		t.Fatalf("重复屏幕文字 = %+v", got.Snapshots[1])
	}
	if got.Snapshots[2].OCR != "alpha beta gamma" {
		t.Fatalf("第三条 OCR = %q", got.Snapshots[2].OCR)
	}

	out, _, _ = runQuery(t, f.env, "snapshots", "--from", "09:30", "--to", "10:30")
	ranged := decode[struct {
		Total     int              `json:"total"`
		Snapshots []snapshotOutput `json:"snapshots"`
	}](t, out)
	if ranged.Total != 1 || len(ranged.Snapshots) != 1 || ranged.Snapshots[0].ID != "20261105-100000.000-0003" {
		t.Fatalf("时间范围 = %+v", ranged)
	}
	if ranged.Snapshots[0].OCR != "" {
		t.Fatal("未带 --ocr 时输出了屏幕文字")
	}

	out, _, _ = runQuery(t, f.env, "snapshots", "--app", "EDITOR")
	byApp := decode[struct {
		Total     int              `json:"total"`
		Snapshots []snapshotOutput `json:"snapshots"`
	}](t, out)
	if byApp.Total != 2 {
		t.Fatalf("应用过滤 = %+v", byApp)
	}

	out, _, _ = runQuery(t, f.env, "snapshots", "--limit", "1")
	latest := decode[struct {
		Snapshots []snapshotOutput `json:"snapshots"`
	}](t, out)
	if len(latest.Snapshots) != 1 || latest.Snapshots[0].ID != "20261105-110000.000-0004" {
		t.Fatalf("最近一条 = %+v", latest.Snapshots)
	}

	out, _, _ = runQuery(t, f.env, "snapshots", "--limit", "10")
	all := decode[struct {
		Snapshots []snapshotOutput `json:"snapshots"`
	}](t, out)
	foundShot := false
	for _, sn := range all.Snapshots {
		if sn.ID == "20261105-090000.000-0001" {
			foundShot = sn.Shot != "" && filepath.IsAbs(sn.Shot)
		}
	}
	if !foundShot {
		t.Fatalf("截图路径缺失: %+v", all.Snapshots)
	}
}

func TestEmptyListsAreArrays(t *testing.T) {
	f := newFixture(t, defaultConfig())
	out, stderr, code := runQuery(t, f.env, "day")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	for _, want := range []string{`"summaries": []`, `"blocks": []`, `"sessions": []`} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %s:\n%s", want, out)
		}
	}
	out, _, _ = runQuery(t, f.env, "snapshots")
	for _, want := range []string{`"snapshots": []`, `"total": 0`, `"returned": 0`} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %s:\n%s", want, out)
		}
	}
	out, _, _ = runQuery(t, f.env, "search", "nothing")
	if !strings.Contains(out, `"matches": []`) {
		t.Fatalf("空匹配不是数组:\n%s", out)
	}
}

func TestUsageErrors(t *testing.T) {
	f := standardFixture(t)
	cases := [][]string{
		{},
		{"bogus"},
		{"status", "--limit", "3"},
		{"days", "--limit", "x"},
		{"day", "--date", "11/05"},
		{"snapshots", "--from", "9"},
		{"search"},
		{"snapshots", "--unknown"},
	}
	for _, args := range cases {
		out, stderr, code := runQuery(t, f.env, args...)
		if code != 2 {
			t.Errorf("%v: 退出码 %d，期望 2", args, code)
		}
		if out != "" {
			t.Errorf("%v: stdout 应为空，得到 %q", args, out)
		}
		if !strings.HasPrefix(stderr, "konseki query: ") || !strings.Contains(stderr, "usage: konseki query") {
			t.Errorf("%v: stderr = %q", args, stderr)
		}
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		out, _, code := runQuery(t, f.env, args...)
		if code != 0 || !strings.HasPrefix(out, "usage: konseki query") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
}

func TestMissingDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	env := Env{Dir: dir, Now: func() time.Time { return fixtureNow }}
	_, stderr, code := runQuery(t, env, "status")
	if code != 1 {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.Contains(stderr, "no Konseki data at "+dir) || !strings.Contains(stderr, "Open Konseki once first.") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("命令创建了数据目录: %v", err)
	}
}

type fileFingerprint struct {
	Path    string
	Size    int64
	Mode    os.FileMode
	ModTime int64
	Hash    string
}

func fingerprint(t *testing.T, root string) map[string]fileFingerprint {
	t.Helper()
	out := map[string]fileFingerprint{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fp := fileFingerprint{Path: path, Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime().UnixNano()}
		if !info.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			fp.Hash = hex.EncodeToString(sum[:])
		}
		out[path] = fp
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCommandsAreReadOnly(t *testing.T) {
	f := standardFixture(t)
	before := fingerprint(t, f.dir)
	commands := [][]string{
		{"status"},
		{"days"},
		{"day", "--date", "2026-11-04"},
		{"snapshots", "--ocr", "--limit", "2"},
		{"search", "alpha", "--days", "7"},
	}
	for _, args := range commands {
		if _, stderr, code := runQuery(t, f.env, args...); code != 0 {
			t.Fatalf("%v 失败: %s", args, stderr)
		}
	}
	after := fingerprint(t, f.dir)
	if len(before) != len(after) {
		t.Fatalf("文件数量变化: %d -> %d", len(before), len(after))
	}
	for path, b := range before {
		a, ok := after[path]
		if !ok || a != b {
			t.Fatalf("%s 被修改: %+v -> %+v", path, b, a)
		}
	}
}

func TestLink(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "Konseki")
	if err := os.WriteFile(exe, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, LinkName)

	if err := Link(dir, exe); err != nil {
		t.Fatalf("新建: %v", err)
	}
	if target, err := os.Readlink(link); err != nil || target != exe {
		t.Fatalf("链接 = %q, %v", target, err)
	}
	beforeLink, _ := os.Lstat(link)
	if err := Link(dir, exe); err != nil {
		t.Fatalf("已经正确: %v", err)
	}

	afterLink, _ := os.Lstat(link)
	if !os.SameFile(beforeLink, afterLink) || !reflect.DeepEqual(beforeLink, afterLink) {
		t.Fatal("正确链接元信息变化")
	}
	other := filepath.Join(t.TempDir(), "Other")
	if err := os.WriteFile(other, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Link(dir, other); err != nil {
		t.Fatalf("替换: %v", err)
	}
	if target, _ := os.Readlink(link); target != other {
		t.Fatalf("替换后指向 %q", target)
	}

	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, LinkName), []byte("not a link"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Link(plain, exe); err == nil {
		t.Fatal("同名普通文件没有被拒绝")
	}
	if b, _ := os.ReadFile(filepath.Join(plain, LinkName)); string(b) != "not a link" {
		t.Fatal("同名普通文件被改动")
	}

	if err := Link(dir, "relative/exe"); err == nil {
		t.Fatal("相对路径没有被拒绝")
	}
}

func TestSnapshotsExcludeToBoundary(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "snapshots", "--from", "09:30", "--to", "10:00")
	if code != 0 {
		t.Fatal(stderr)
	}
	got := decode[struct {
		Total int `json:"total"`
	}](t, out)
	if got.Total != 0 {
		t.Fatalf("上界记录未排除: %s", out)
	}
}

func TestReviewRegressions(t *testing.T) {
	f := standardFixture(t)
	for _, args := range [][]string{{"search", "help"}, {"search", "--", "--help"}, {"search", "--", "-h"}} {
		out, stderr, code := runQuery(t, f.env, args...)
		if code != 0 {
			t.Fatal(stderr)
		}
		decode[searchOutput](t, out)
	}
	for _, args := range [][]string{
		{"--help"}, {"help"},
		{"status", "--help"}, {"days", "--help"}, {"day", "--help"},
		{"snapshots", "--help"}, {"search", "--help"},
		{"search", "days", "--help"}, {"search", "limit", "--help"},
	} {
		out, _, code := runQuery(t, f.env, args...)
		if code != 0 || !strings.Contains(out, "usage:") {
			t.Fatalf("%v: code=%d output=%q", args, code, out)
		}
	}
	for _, cmd := range []string{"day", "days", "status", "snapshots"} {
		_, _, code := runQuery(t, f.env, cmd, "2020-01-01")
		if code != 2 {
			t.Fatal(cmd, code)
		}
	}
	for _, prefix := range []string{"Ⱥ", "İ", "中文"} {
		got, ok := snippet(strings.Repeat(prefix, 100)+" needle", "needle")
		if !ok || !strings.Contains(got, "needle") {
			t.Fatal(got)
		}
	}
	for _, enabled := range []bool{true, false} {
		sf, _ := sensitiveFixture(t, enabled)
		out, _, _ := runQuery(t, sf.env, "search", "alice@example.com")
		got := decode[searchOutput](t, out)
		if strings.Contains(got.Query, "alice@example.com") == enabled {
			t.Fatal(got.Query)
		}
	}
	dir := t.TempDir()
	tmp := filepath.Join(dir, ".konseki.tmp")
	os.WriteFile(tmp, []byte("keep"), 0600)
	if err := Link(dir, "/old"); err != nil {
		t.Fatal(err)
	}
	if err := Link(dir, "/new"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(tmp); string(b) != "keep" {
		t.Fatal("用户文件变化")
	}
}
func TestDSTBounds(t *testing.T) {
	old := time.Local
	defer func() { time.Local = old }()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	time.Local = loc
	for _, day := range []string{"2026-03-08", "2026-11-01"} {
		start, end, err := dayBounds(day, "09:00", "10:00", true, true)
		if err != nil || start.Hour() != 9 || end.Hour() != 10 {
			t.Fatal(start, end, err)
		}
		if _, _, err := dayBounds(day, "10:00", "09:00", true, true); err == nil {
			t.Fatal("逆序被接受")
		}
	}
}

func TestSessionOnlyDay(t *testing.T) {
	f := newFixture(t, defaultConfig())
	f.addSession(capture.Session{ID: "midnight", Start: fixtureNow.Add(-time.Hour), End: fixtureNow})
	out, stderr, code := runQuery(t, f.env, "days")
	got := decode[struct{ Days []dayCount }](t, out)
	if code != 0 || len(got.Days) != 1 || got.Days[0].Snapshots != 0 || got.Days[0].Sessions != 1 {
		t.Fatal(out, stderr)
	}
}
