/*
 * [INPUT]: 依赖临时摘要存储和假模型服务。
 * [OUTPUT]: 验证摘要生成、存储、清除和脱敏。
 * [POS]: 摘要模块的行为回归测试，不请求真实服务。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package summary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/llm"
	"konseki/internal/redact"
)

// fakeLLM 起一个 OpenAI 兼容的假服务端。
//
// 用假服务端而不是打真接口：这里要验证的是「发出去的文本确实脱敏了」
// 和「回来的占位符确实还原了」，跟模型说什么无关。
func fakeLLM(t *testing.T, reply string) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var sb strings.Builder
		for _, m := range body.Messages {
			sb.WriteString(m.Content)
		}
		got = sb.String()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model": "fake",
			"choices": []map[string]any{{
				"message": map[string]string{"content": reply},
			}},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestGenerateRedactsBeforeSending(t *testing.T) {
	srv, sent := fakeLLM(t, `{"headline":"改了配置","summary":"在改 .env","topics":["env"]}`)

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 4, 14, 0, 0, 0, time.Local)

	gen := New(store, llm.New(10*time.Second), LLMConfig{
		Kind:    llm.OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		Model:   "fake",
		Redact:  redact.Options{Secrets: true, Personal: true},
	}, nil)

	// 构造的敏感值刻意用变量拼，不写字面量：否则源码里就有一份
	// 「长得像真凭据」的字符串，各类密钥扫描器会把仓库本身报成泄漏。
	email := "alice" + "@" + "example.com"
	dbPassword := "hunter2-not-a-real-one"
	apiKey := "abcdef0123456789abcdef"

	blocks := []capture.Block{{
		Start: base, End: base.Add(30 * time.Minute),
		App: "Code", Titles: []string{"编辑器 — .env"}, Samples: 5,
	}}
	ocr := map[string]string{
		"编辑器 — .env": "DB_PASSWORD=" + dbPassword + "\nMAIL=" + email + "\nAPI_KEY=" + apiKey,
	}

	entries, err := gen.GenerateDay(t.Context(), blocks, ocr, "2026-11-04", time.Hour, true)
	if err != nil {
		t.Fatalf("GenerateDay: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("期望 1 条摘要，得到 %d", len(entries))
	}

	// 关键断言：真正发出去的文本里不能有原文。
	for _, secret := range []string{email, dbPassword, apiKey} {
		if strings.Contains(*sent, secret) {
			t.Errorf("原文 %q 被发给了模型", secret)
		}
	}
	// 而且必须真的换成了占位符，不能是「什么都没遮」蒙混过关。
	// SECRET 会先于 PASSWORD 命中 DB_PASSWORD= 这种形式（前者规则更宽），
	// 所以这里只断言「有占位符」，不锁死是哪个 kind。
	for _, want := range []string{"{{EMAIL_", "{{SECRET_"} {
		if !strings.Contains(*sent, want) {
			t.Errorf("缺少占位符 %q，发出去的内容：%s", want, *sent)
		}
	}

	e := entries[0]
	if e.Headline != "改了配置" {
		t.Errorf("headline = %q", e.Headline)
	}
	if e.Apps[0] != "Code" {
		t.Errorf("apps = %v", e.Apps)
	}
	if e.Usage.Input != 10 || e.Usage.Output != 5 {
		t.Errorf("usage = %+v", e.Usage)
	}
	if e.Redactions == 0 {
		t.Error("Redactions 应该记下遮了几处")
	}
}

// 模型在答案里引用占位符时，落盘的摘要必须是原文。
func TestRestoresPlaceholdersInReply(t *testing.T) {
	srv, _ := fakeLLM(t, `{"headline":"回复","summary":"联系人 {{EMAIL_aaaaaaaa}} 在等回复"}`)

	store, _ := NewStore(t.TempDir())
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)

	gen := New(store, llm.New(10*time.Second), LLMConfig{
		Kind:    llm.OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		Model:   "fake",
		Redact:  redact.Options{Secrets: true, Personal: true},
	}, nil)

	blocks := []capture.Block{{
		Start: base, End: base.Add(10 * time.Minute),
		App: "Mail", Titles: []string{"收件箱"}, Samples: 2,
	}}
	ocr := map[string]string{"收件箱": "From: someone" + "@" + "example.com 请回复"}

	entries, err := gen.GenerateDay(t.Context(), blocks, ocr, "2026-11-04", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	// 模型自己编的占位符不在反查表里，应当原样保留，不能被吃掉。
	if !strings.Contains(entries[0].Summary, "{{EMAIL_aaaaaaaa}}") {
		t.Errorf("未知占位符被改写：%q", entries[0].Summary)
	}
}

func TestStoreUpsertReplaces(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	base := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	e := Entry{ID: "x", Day: "2026-11-04", Start: base, End: base.Add(time.Hour),
		Headline: "第一版", CreatedAt: time.Now()}
	if err := store.Upsert(e); err != nil {
		t.Fatal(err)
	}
	e.Headline = "第二版"
	if err := store.Upsert(e); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("2026-11-04")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Headline != "第二版" {
		t.Fatalf("同 ID 应该覆盖，得到 %+v", got)
	}
}

func TestParseStructuredToleratesProse(t *testing.T) {
	// 模型没按 JSON 输出时，整段散文要当成 summary，不能丢
	s := parseStructured("这段时间主要在改登录页。")
	if s.Summary != "这段时间主要在改登录页。" {
		t.Errorf("summary = %q", s.Summary)
	}
	// 带代码栏的也要能抠出来
	s = parseStructured("```json\n{\"headline\":\"h\",\"summary\":\"s\"}\n```")
	if s.Headline != "h" || s.Summary != "s" {
		t.Errorf("解析失败：%+v", s)
	}
}

func TestSplitWindowsAlignsToHour(t *testing.T) {
	base := time.Date(2026, 11, 4, 9, 47, 0, 0, time.Local)
	blocks := []capture.Block{
		{Start: base, End: base.Add(20 * time.Minute), App: "A"},
		{Start: base.Add(40 * time.Minute), End: base.Add(50 * time.Minute), App: "B"},
	}
	ws := splitWindows(blocks, time.Hour)
	if len(ws) != 2 {
		t.Fatalf("期望 2 个窗口，得到 %d", len(ws))
	}
	// 第一个窗口必须对齐到 9:00，而不是 9:47
	if ws[0].start.Hour() != 9 || ws[0].start.Minute() != 0 {
		t.Errorf("窗口起点没对齐整点：%v", ws[0].start)
	}
	if ws[1].start.Hour() != 10 {
		t.Errorf("第二个窗口应在 10 点：%v", ws[1].start)
	}
}

func TestEntryIDStable(t *testing.T) {
	base := time.Date(2026, 11, 4, 14, 0, 0, 0, time.Local)
	a := EntryID("2026-11-04", base)
	b := EntryID("2026-11-04", base)
	if a != b {
		t.Fatalf("同一时间算出了不同 ID：%q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "2026-11-04-") {
		t.Errorf("ID 前缀不对：%q", a)
	}
}

func TestClearPreservesOtherFiles(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Entry{ID: "one", Day: "2026-11-04"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "summaries", "notes.txt")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := s.Days(); len(got) != 0 {
		t.Fatalf("清除后仍有摘要: %v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenStoreReadOnlyAndDaysFilter(t *testing.T) {
	root := t.TempDir()
	ro := OpenStore(root)
	entry := Entry{ID: "x", Day: "2026-11-04", Headline: "h"}
	if err := ro.Upsert(entry); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("OpenStore.Upsert err = %v, want ErrReadOnly", err)
	}
	if err := ro.Clear(); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("OpenStore.Clear err = %v, want ErrReadOnly", err)
	}
	if _, err := os.Stat(filepath.Join(root, "summaries")); !os.IsNotExist(err) {
		t.Fatalf("OpenStore should not create summaries dir, err=%v", err)
	}

	rw, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := rw.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	if err := rw.Upsert(Entry{ID: "", Day: "2026-11-04"}); err == nil {
		t.Fatal("expected error for empty ID")
	}
	if err := rw.Upsert(Entry{ID: "x", Day: "bad-day"}); err == nil {
		t.Fatal("expected error for invalid Day")
	}
	sumDir := filepath.Join(root, "summaries")
	if err := os.Mkdir(filepath.Join(sumDir, "2026-11-05.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sumDir, "notes.jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sumDir, "2026-11-06.jsonl.tmp"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := rw.Days(); !reflect.DeepEqual(got, []string{"2026-11-04"}) {
		t.Fatalf("Days() = %v, want [2026-11-04]", got)
	}
}

func TestUpsertReturnsErrorWithoutWipingOnReadFailure(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	dayPath := filepath.Join(root, "summaries", "2026-11-04.jsonl")
	if err := os.Mkdir(dayPath, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(dayPath, "keep")
	if err := os.WriteFile(sentinel, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Entry{ID: "x", Day: "2026-11-04", Headline: "new"}); err == nil {
		t.Fatal("expected Upsert error when dayPath cannot be read")
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("existing path disturbed on Upsert read failure: %v", err)
	}
}

func TestRestoreAfterJSONParseHandlesQuotesBackslashesAndNewlines(t *testing.T) {
	wordSecret := "proj\"quote\\slash\nnextline"
	pemSecret := "-----BEGIN RSA PRIVATE KEY----- LINE1 -----END RSA PRIVATE KEY-----"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var sent strings.Builder
		for _, m := range body.Messages {
			sent.WriteString(m.Content)
		}
		s := sent.String()
		var wordPH, pemPH string
		if i := strings.Index(s, "{{WORD_"); i >= 0 {
			if j := strings.Index(s[i:], "}}"); j >= 0 {
				wordPH = s[i : i+j+2]
			}
		}
		if i := strings.Index(s, "{{PRIVATE_KEY_"); i >= 0 {
			if j := strings.Index(s[i:], "}}"); j >= 0 {
				pemPH = s[i : i+j+2]
			}
		}
		if wordPH == "" || pemPH == "" {
			t.Fatalf("expected placeholders in prompt, got wordPH=%q pemPH=%q", wordPH, pemPH)
		}
		replyObj := map[string]any{
			"headline":  "Edited " + wordPH,
			"summary":   "Key " + pemPH + " / " + wordPH,
			"open":      []string{"finish " + wordPH},
			"topics":    []string{wordPH},
			"files":     []string{wordPH + ".env"},
			"people":    []string{wordPH},
			"decisions": []string{"rotate " + pemPH},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake",
			"choices": []map[string]any{{"message": map[string]string{"content": mustJSON(t, replyObj)}}},
		})
	}))
	defer srv.Close()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 4, 14, 0, 0, 0, time.Local)
	gen := New(store, llm.New(10*time.Second), LLMConfig{
		Kind:    llm.OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		Model:   "fake",
		Redact:  redact.Options{Secrets: true, Words: []string{wordSecret}},
	}, nil)

	title := "edit " + wordSecret
	blocks := []capture.Block{{
		Start: base, End: base.Add(20 * time.Minute),
		App: "Code", Titles: []string{title}, Samples: 1,
	}}
	ocr := map[string]string{title: "key " + pemSecret}

	entries, err := gen.GenerateDay(context.Background(), blocks, ocr, "2026-11-04", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Headline != "Edited "+wordSecret {
		t.Errorf("Headline = %q", e.Headline)
	}
	if e.Summary != "Key "+pemSecret+" / "+wordSecret {
		t.Errorf("Summary = %q", e.Summary)
	}
	if !reflect.DeepEqual(e.Open, []string{"finish " + wordSecret}) ||
		!reflect.DeepEqual(e.Topics, []string{wordSecret}) ||
		!reflect.DeepEqual(e.Files, []string{wordSecret + ".env"}) ||
		!reflect.DeepEqual(e.People, []string{wordSecret}) ||
		!reflect.DeepEqual(e.Decisions, []string{"rotate " + pemSecret}) {
		t.Errorf("restored slice fields wrong: %+v", e)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPerWindowIsolatedRedactionsAndCancellationStatus(t *testing.T) {
	srv, _ := fakeLLM(t, `{"headline":"ok","summary":"done"}`)
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	w1Start := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	w2Start := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	blocks := []capture.Block{
		{Start: w1Start, End: w1Start.Add(10 * time.Minute), App: "A", Titles: []string{"t1"}},
		{Start: w2Start, End: w2Start.Add(10 * time.Minute), App: "B", Titles: []string{"t2"}},
	}
	ocr := map[string]string{
		"t1": "a@example.com",
		"t2": "b@example.com c@example.com",
	}

	gen := New(store, llm.New(10*time.Second), LLMConfig{
		Kind:    llm.OpenAICompatible,
		BaseURL: srv.URL + "/v1",
		Model:   "fake",
		Redact:  redact.Options{Personal: true},
	}, nil)

	entries, err := gen.GenerateDay(context.Background(), blocks, ocr, "2026-11-04", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Redactions != 1 || entries[1].Redactions != 2 {
		t.Fatalf("redactions not isolated per window: w1=%d w2=%d", entries[0].Redactions, entries[1].Redactions)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = gen.GenerateDay(ctx, blocks, ocr, "2026-11-04", time.Hour, true)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	st := gen.Status()
	if st.Running {
		t.Fatal("Status().Running remained true after canceled GenerateDay")
	}
	if !strings.Contains(st.LastErr, context.Canceled.Error()) {
		t.Fatalf("Status().LastErr = %q, want context.Canceled", st.LastErr)
	}
}

func TestSplitWindowsSizesAndMixedTimezones(t *testing.T) {
	b1 := time.Date(2026, 11, 4, 9, 47, 0, 0, time.Local)
	b2 := time.Date(2026, 11, 4, 9, 15, 0, 0, time.Local)
	ws30 := splitWindows([]capture.Block{
		{Start: b2, End: b2.Add(5 * time.Minute), App: "A"},
		{Start: b1, End: b1.Add(5 * time.Minute), App: "B"},
	}, 30*time.Minute)
	if len(ws30) != 2 {
		t.Fatalf("30m windows = %d, want 2", len(ws30))
	}
	if ws30[0].start.Hour() != 9 || ws30[0].start.Minute() != 0 || ws30[1].start.Hour() != 9 || ws30[1].start.Minute() != 30 {
		t.Fatalf("30m alignment wrong: %v, %v", ws30[0].start, ws30[1].start)
	}

	ws2h := splitWindows([]capture.Block{
		{Start: b1, End: b1.Add(5 * time.Minute), App: "A"},
		{Start: time.Date(2026, 11, 4, 10, 5, 0, 0, time.Local), End: time.Date(2026, 11, 4, 10, 15, 0, 0, time.Local), App: "B"},
	}, 2*time.Hour)
	if len(ws2h) != 2 {
		t.Fatalf("2h windows = %d, want 2", len(ws2h))
	}
	if ws2h[0].start.Hour() != 8 || ws2h[1].start.Hour() != 10 {
		t.Fatalf("2h alignment wrong: %v, %v", ws2h[0].start, ws2h[1].start)
	}

	tLocal := time.Date(2026, 11, 4, 9, 10, 0, 0, time.Local)
	tUTC := tLocal.Add(15 * time.Minute).UTC()
	tFixed := tLocal.Add(30 * time.Minute).In(time.FixedZone("Z", 3600))
	wsMix := splitWindows([]capture.Block{
		{Start: tLocal, End: tLocal.Add(5 * time.Minute), App: "L"},
		{Start: tUTC, End: tUTC.Add(5 * time.Minute), App: "U"},
		{Start: tFixed, End: tFixed.Add(5 * time.Minute), App: "F"},
	}, time.Hour)
	if len(wsMix) != 1 || len(wsMix[0].blocks) != 3 {
		t.Fatalf("mixed timezone blocks were not grouped into 1 window: %+v", wsMix)
	}
}

func TestClearGenerationFencingRejectsInFlightGenerateDay(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake",
			"choices": []map[string]any{{"message": map[string]string{"content": `{"headline":"stale","summary":"should not stick"}`}}},
		})
	}))
	defer srv.Close()

	gen := New(store, llm.New(10*time.Second), LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "fake"}, nil)
	start := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	blocks := []capture.Block{{Start: start, End: start.Add(5 * time.Minute), App: "Code"}}

	errCh := make(chan error, 1)
	go func() {
		_, err := gen.GenerateDay(context.Background(), blocks, nil, "2026-11-04", time.Hour, true)
		errCh <- err
	}()
	<-started
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errCh; !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("GenerateDay err = %v, want ErrStaleWrite", err)
	}
	if days := store.Days(); len(days) != 0 {
		t.Fatalf("stale write survived Clear: %v", days)
	}
}

func TestWindowCoverageUpdatesSecondHalfActivity(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake",
			"choices": []map[string]any{{"message": map[string]string{"content": fmt.Sprintf(`{"headline":"pass-%d","summary":"updated"}`, calls)}}},
		})
	}))
	defer srv.Close()

	gen := New(store, llm.New(10*time.Second), LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "fake"}, nil)
	day := "2026-11-04"
	b1 := capture.Block{
		Start: time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local),
		End:   time.Date(2026, 11, 4, 10, 5, 0, 0, time.Local),
		App:   "Code",
	}
	if _, err := gen.GenerateDay(context.Background(), []capture.Block{b1}, nil, day, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	entries, err := store.Load(day)
	if err != nil || len(entries) != 1 || entries[0].Headline != "pass-1" {
		t.Fatalf("initial entries = %+v, err = %v", entries, err)
	}
	if HasPendingWindows(day, []capture.Block{b1}, entries, time.Hour) {
		t.Fatal("HasPendingWindows reported true for unchanged window")
	}
	if out, err := gen.GenerateDay(context.Background(), []capture.Block{b1}, nil, day, time.Hour, false); err != nil || len(out) != 0 {
		t.Fatalf("expected unchanged window skip, got out=%+v err=%v", out, err)
	}

	b2 := capture.Block{
		Start: time.Date(2026, 11, 4, 10, 20, 0, 0, time.Local),
		End:   time.Date(2026, 11, 4, 10, 40, 0, 0, time.Local),
		App:   "Terminal",
	}
	blocks2 := []capture.Block{b1, b2}
	if !HasPendingWindows(day, blocks2, entries, time.Hour) {
		t.Fatal("HasPendingWindows missed new second-half block")
	}
	if _, err := gen.GenerateDay(context.Background(), blocks2, nil, day, time.Hour, false); err != nil {
		t.Fatal(err)
	}
	entries, err = store.Load(day)
	if err != nil || len(entries) != 1 || entries[0].Headline != "pass-2" || !entries[0].CoveredUntil.Equal(b2.End) {
		t.Fatalf("second-half window not updated: %+v err=%v", entries, err)
	}
}

func TestStoreStrictUpsertOnCorruptJSONAndCrossStoreConcurrency(t *testing.T) {
	root := t.TempDir()
	s1, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const day = "2026-11-04"
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	base := time.Date(2026, 11, 4, 8, 0, 0, 0, time.Local)
	for i := range 20 {
		wg.Add(1)
		st := s1
		if i%2 == 1 {
			st = s2
		}
		go func(i int, st *Store) {
			defer wg.Done()
			errs <- st.Upsert(Entry{
				ID:        fmt.Sprintf("e-%02d", i),
				Day:       day,
				Start:     base.Add(time.Duration(i) * time.Minute),
				End:       base.Add(time.Duration(i+1) * time.Minute),
				Headline:  fmt.Sprintf("h-%02d", i),
				CreatedAt: base,
			})
		}(i, st)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Upsert failed: %v", err)
		}
	}
	loaded, err := s1.Load(day)
	if err != nil || len(loaded) != 20 {
		t.Fatalf("concurrent Upsert lost entries: len=%d err=%v", len(loaded), err)
	}

	dayPath := filepath.Join(root, "summaries", day+".jsonl")
	rawBefore, err := os.ReadFile(dayPath)
	if err != nil {
		t.Fatal(err)
	}
	withCorrupt := append(append([]byte{}, rawBefore...), []byte("{corrupt-json\n")...)
	if err := os.WriteFile(dayPath, withCorrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s1.Upsert(Entry{ID: "new-id", Day: day, Start: base, End: base.Add(time.Minute)}); err == nil {
		t.Fatal("expected strict Upsert failure on corrupt JSON line")
	}
	rawAfter, err := os.ReadFile(dayPath)
	if err != nil || string(rawAfter) != string(withCorrupt) {
		t.Fatalf("corrupt Upsert modified existing file: %v", err)
	}
}

func TestCrossInstanceClearFencingAndSetConfigAbort(t *testing.T) {
	root := t.TempDir()
	sA, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sB, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	const day = "2026-11-04"
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	if err := sA.Upsert(Entry{ID: "init", Day: day, Start: base, End: base.Add(time.Minute), Headline: "init"}); err != nil {
		t.Fatal(err)
	}
	gen := sA.Generation()
	if err := sB.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := sA.UpsertIfGen(Entry{ID: "stale", Day: day, Start: base, End: base.Add(time.Minute), Headline: "stale"}, gen); !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("cross-instance UpsertIfGen err = %v, want ErrStaleWrite", err)
	}
	if days := sB.Days(); len(days) != 0 {
		t.Fatalf("cleared store polluted by another instance: %v", days)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m1",
			"choices": []map[string]any{{"message": map[string]string{"content": `{"headline":"old-model","summary":"stale"}`}}},
		})
	}))
	defer srv.Close()

	cfgA := LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "m1"}
	cfgB := LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "m2"}
	g := New(sA, llm.New(10*time.Second), cfgA, nil)
	errCh := make(chan error, 1)
	go func() {
		_, err := g.GenerateDay(context.Background(), []capture.Block{{Start: base, End: base.Add(5 * time.Minute), App: "Code"}}, nil, day, time.Hour, true)
		errCh <- err
	}()
	<-started
	g.SetConfig(cfgB)
	close(release)
	if err := <-errCh; !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("GenerateDay after SetConfig err = %v, want ErrStaleWrite", err)
	}
	if days := sA.Days(); len(days) != 0 {
		t.Fatalf("stale model output survived SetConfig: %v", days)
	}
}

func TestSetConfigWhileWaitingForStoreLockBlocksStaleCommit(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	reqStarted := make(chan struct{})
	releaseReq := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(reqStarted)
		<-releaseReq
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m1",
			"choices": []map[string]any{{"message": map[string]string{"content": `{"headline":"stale-locked","summary":"should-abort"}`}}},
		})
	}))
	defer srv.Close()

	const day = "2026-11-04"
	base := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	cfgA := LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "m1"}
	cfgB := LLMConfig{Kind: llm.OpenAICompatible, BaseURL: srv.URL + "/v1", Model: "m2"}
	g := New(s, llm.New(10*time.Second), cfgA, nil)

	errCh := make(chan error, 1)
	go func() {
		_, err := g.GenerateDay(context.Background(), []capture.Block{{Start: base, End: base.Add(5 * time.Minute), App: "Code"}}, nil, day, time.Hour, true)
		errCh <- err
	}()
	<-reqStarted
	unlock, err := lockStoreFile(filepath.Join(root, "summaries"))
	if err != nil {
		close(releaseReq)
		t.Fatal(err)
	}
	close(releaseReq)
	time.Sleep(40 * time.Millisecond)

	g.SetConfig(cfgB)
	select {
	case err := <-errCh:
		unlock()
		t.Fatalf("GenerateDay returned before store lock was released: %v", err)
	default:
	}
	unlock()

	if err := <-errCh; !errors.Is(err, ErrStaleWrite) {
		t.Fatalf("GenerateDay err after lock release = %v, want ErrStaleWrite", err)
	}
	entries, err := s.Load(day)
	if err != nil || len(entries) != 0 {
		t.Fatalf("stale summary committed after waiting on store lock: entries=%+v err=%v", entries, err)
	}
}
