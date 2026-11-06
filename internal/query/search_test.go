/*
 * [INPUT]: 依赖两天夹具和 search 命令。
 * [OUTPUT]: 验证搜索的字段顺序、合并、排序、截断、遮蔽后丢弃和 flag 位置。
 * [POS]: query 包搜索行为的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package query

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"konseki/internal/summary"
)

type searchOutput struct {
	Query     string `json:"query"`
	From      string `json:"from"`
	To        string `json:"to"`
	Total     int    `json:"total"`
	Returned  int    `json:"returned"`
	Truncated bool   `json:"truncated"`
	Matches   []struct {
		Source  string `json:"source"`
		Day     string `json:"day"`
		ID      string `json:"id"`
		Start   string `json:"start"`
		End     string `json:"end"`
		TS      string `json:"ts"`
		App     string `json:"app"`
		Title   string `json:"title"`
		Field   string `json:"field"`
		Snippet string `json:"snippet"`
		Shot    string `json:"shot"`
		Count   int    `json:"count"`
	} `json:"matches"`
}

func TestSearchOrdersAndBounds(t *testing.T) {
	f := standardFixture(t)
	out, stderr, code := runQuery(t, f.env, "search", "alpha")
	if code != 0 {
		t.Fatalf("退出码 %d: %s", code, stderr)
	}
	got := decode[searchOutput](t, out)
	if got.Query != "alpha" || got.From != "2026-10-30" || got.To != "2026-11-05" {
		t.Fatalf("头部 = %+v", got)
	}
	if got.Total != 3 || got.Returned != 3 || got.Truncated {
		t.Fatalf("计数 = %+v", got)
	}
	if got.Matches[0].Source != "snapshot" || got.Matches[0].Day != "2026-11-05" || got.Matches[0].ID != "20261105-110000.000-0004" {
		t.Fatalf("第一条 = %+v", got.Matches[0])
	}
	if got.Matches[1].Source != "summary" || got.Matches[1].ID != "2026-11-04-cccc" || got.Matches[1].Field != "summary" {
		t.Fatalf("第二条 = %+v", got.Matches[1])
	}
	if got.Matches[2].Source != "snapshot" || got.Matches[2].Day != "2026-11-04" {
		t.Fatalf("第三条 = %+v", got.Matches[2])
	}
	if !strings.Contains(got.Matches[2].Snippet, "alpha") {
		t.Fatalf("片段 = %q", got.Matches[2].Snippet)
	}

	out, _, _ = runQuery(t, f.env, "search", "alpha", "--days", "1")
	got = decode[searchOutput](t, out)
	if got.From != "2026-11-05" || got.Total != 1 {
		t.Fatalf("一天范围 = %+v", got)
	}

	out, _, _ = runQuery(t, f.env, "search", "alpha", "--limit", "1")
	got = decode[searchOutput](t, out)
	if got.Total != 3 || got.Returned != 1 || !got.Truncated {
		t.Fatalf("截断 = %+v", got)
	}
}

func TestSearchFieldOrder(t *testing.T) {
	f := standardFixture(t)
	out, _, _ := runQuery(t, f.env, "search", "查询")
	got := decode[searchOutput](t, out)
	if len(got.Matches) != 1 || got.Matches[0].Field != "headline" {
		t.Fatalf("headline 优先 = %+v", got.Matches)
	}

	f.addSnapshot(time.Date(2026, 11, 5, 15, 0, 0, 0, time.Local), "20261105-150000.000-0006",
		"Editor", "com.example.editor", "both needle title", "both needle ocr", false)
	out, _, _ = runQuery(t, f.env, "search", "needle")
	got = decode[searchOutput](t, out)
	if len(got.Matches) != 1 || got.Matches[0].Field != "title" {
		t.Fatalf("title 先于 ocr = %+v", got.Matches)
	}
}

func TestSearchMergesSameMatch(t *testing.T) {
	f := newFixture(t, defaultConfig())
	base := time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	for i := range []string{"a", "b", "c"} {
		f.addSnapshot(base.Add(time.Duration(i)*time.Minute), "20261105-09000"+string(rune('0'+i))+".000-000"+string(rune('1'+i)),
			"Editor", "com.example.editor", "needle title", "needle text", false)
	}
	out, _, code := runQuery(t, f.env, "search", "needle")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	got := decode[searchOutput](t, out)
	if got.Total != 1 || got.Returned != 1 || got.Matches[0].Count != 3 {
		t.Fatalf("合并 = %+v", got)
	}
	if got.Matches[0].ID != "20261105-090002.000-0003" {
		t.Fatalf("保留的 id = %q", got.Matches[0].ID)
	}
}

func TestSearchDropsMatchInsideRedactedValue(t *testing.T) {
	f := newFixture(t, defaultConfig())
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	f.addEntry(summary.Entry{
		ID: "secret", Day: "2026-11-05", Start: at, End: at.Add(time.Hour),
		Headline: "alice@example.com", Summary: "无内容",
	})
	out, _, code := runQuery(t, f.env, "search", "alice")
	if code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	if strings.Contains(out, "alice@example.com") {
		t.Fatalf("输出了被遮的值: %s", out)
	}
	got := decode[searchOutput](t, out)
	if got.Total != 0 || len(got.Matches) != 0 {
		t.Fatalf("应丢弃落在被遮值里的命中: %+v", got)
	}
}

func TestSearchFlagsBeforeAndAfterText(t *testing.T) {
	f := standardFixture(t)
	before, _, _ := runQuery(t, f.env, "search", "--days", "7", "alpha")
	after, _, _ := runQuery(t, f.env, "search", "alpha", "--days", "7")
	if before != after {
		t.Fatalf("flag 位置改变了输出:\n%s\n---\n%s", before, after)
	}
	var got searchOutput
	if err := json.Unmarshal([]byte(before), &got); err != nil {
		t.Fatal(err)
	}
	if got.Query != "alpha" || got.Total != 3 {
		t.Fatalf("结果 = %+v", got)
	}
}
