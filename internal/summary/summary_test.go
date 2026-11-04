package summary

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
