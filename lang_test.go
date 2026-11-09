/**
 * [INPUT]: 依赖界面文本、白名单和注入的用户数据。
 * [OUTPUT]: 验证小票与设置三子页中英混排；提供用户数据剔除、两张放行表和 checkPageLanguage 页面检查。
 * [POS]: 根模块的文案验收工具。页面级语言测试先剔除用户数据，再用它判断界面文案是否混入另一种语言。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
)

var latinWord = regexp.MustCompile(`[A-Za-z]{2,}`)

// ---------- 放行表与页面检查 ----------

// timelineAllow 只放行品牌名：时间轴页没有技术术语。
var timelineAllow = []string{"痕跡", "Konseki"}

// settingsAllow 在品牌名之外放行设置页的单位、技术术语、语言名、
// 默认敏感应用的 bundle ID 和产品名。
var settingsAllow = append(append([]string{}, timelineAllow...),
	"MB", "ms", "API", "bundle", "ID", "English", "简体中文", "zh-Hans, en-US",
	"com.1password.1password", "com.apple.keychainaccess", "com.bitwarden.desktop",
	"Agent", "Claude Code", "Codex", "SKILL.md", "Mac",
)

// checkPageLanguage 剔除应用目录、技能命令和调用方给的用户数据，再检查当前页面。
// 时间轴页（tab 0）用 timelineAllow，设置页用 settingsAllow，宽表不放松时间轴。
func checkPageLanguage(t *testing.T, tt *ui.Tester, a *app, lang string, user ...string) {
	t.Helper()
	user = append(user, statusCmd(a.appDir), agentSkillInstall, agentSkillRepo, a.cfgNow.Recorder.Dir(a.appDir), a.legacyDir(), a.appDir)
	sort.SliceStable(user, func(i, j int) bool { return len(user[i]) > len(user[j]) })
	allow := settingsAllow
	if a.tab == 0 {
		allow = timelineAllow
	}
	texts := tt.Texts()
	if bad := langViolations(stripUserData(texts, user...), lang, allow); len(bad) > 0 {
		t.Fatalf("界面混入其他语言 %q，文本：%q", bad, texts)
	}
}

// langViolations 返回不属于 lang 的文本片段。lang 只认 zh-Hans 和 en，
// 写错时直接 panic，免得检查静默放行。
func langViolations(texts []string, lang string, allow []string) []string {
	var out []string
	for _, s := range stripUserData(texts, allow...) {
		switch lang {
		case "zh-Hans":
			out = append(out, latinWord.FindAllString(s, -1)...)
		case "en":
			for _, r := range s {
				if isCJK(r) {
					out = append(out, string(r))
				}
			}
		default:
			panic("langViolations: 不支持的语言 " + lang)
		}
	}
	return out
}

// stripUserData 把 data 出现的位置换成空格。换成空格而不是删掉，
// 是为了不让两侧的拉丁字母拼成一个本不存在的词。
func stripUserData(texts []string, data ...string) []string {
	out := make([]string, len(texts))
	for i, s := range texts {
		for _, d := range data {
			if d != "" {
				s = strings.ReplaceAll(s, d, " ")
			}
		}
		out[i] = s
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef
}

func TestLangViolations(t *testing.T) {
	cases := []struct {
		name, text, lang string
		allow, data      []string
		bad              bool
	}{
		{"纯中文", "你好", "zh-Hans", nil, nil, false},
		{"中文混入英文", "今天 Today", "zh-Hans", nil, nil, true},
		{"白名单放行", "痕跡 Konseki", "zh-Hans", []string{"Konseki"}, nil, false},
		{"白名单两侧不拼词", "aKonsekib", "zh-Hans", []string{"Konseki"}, nil, false},
		{"纯英文", "Hello", "en", nil, nil, false},
		{"英文混入中文", "Today 今天", "en", nil, nil, true},
		{"英文混入全角标点", "Hello，", "en", nil, nil, true},
		{"剔除用户数据", "Hello 今天", "en", nil, []string{"今天"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := langViolations(stripUserData([]string{c.text}, c.data...), c.lang, c.allow)
			if (len(got) > 0) != c.bad {
				t.Fatalf("%q 的违规片段是 %q", c.text, got)
			}
		})
	}
}

func TestEmptyTimelineLanguages(t *testing.T) {
	// 空状态和有数据状态分别检查界面文案。
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			tt := ui.NewTester(a.view, 780, 1000)
			tt.SetLocale(lang)
			if bad := langViolations(tt.Texts(), lang, timelineAllow); len(bad) != 0 {
				t.Fatalf("界面混入其他语言 %q，文本：%q", bad, tt.Texts())
			}
			want := "今天还没有记录"
			if lang == "en" {
				want = "No activity recorded today"
			}
			if !tt.HasText(want) {
				t.Fatalf("空状态缺少 %q：%q", want, tt.Texts())
			}
		})
	}
}

func TestReceiptLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, tt := receiptTestApp(t)
			a.expanded["2026-11-04-1300"] = true
			tt.SetLocale(lang)
			tt.Advance(time.Second)
			var user []string
			for _, b := range a.blocks {
				user = append(user, b.App)
				user = append(user, b.Titles...)
			}
			for _, s := range a.sumEntries {
				user = append(user, s.Headline, s.Summary)
				user = append(user, s.Apps...)
				user = append(user, s.Topics...)
				user = append(user, s.Open...)
				user = append(user, s.Decisions...)
				user = append(user, s.Files...)
				user = append(user, s.People...)
			}
			sort.SliceStable(user, func(i, j int) bool { return len(user[i]) > len(user[j]) })
			if bad := langViolations(stripUserData(tt.Texts(), user...), lang, timelineAllow); len(bad) > 0 {
				t.Fatalf("界面混入其他语言 %q，文本：%q", bad, tt.Texts())
			}
		})
	}
}

func TestLanguageSetting(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	selected := ""
	a.setLocale = func(lang string) { selected = lang }
	a.setLanguage("en")
	if selected != "en" || a.cfg.Get().Language != "en" || a.cfgNow.Language != "en" {
		t.Fatalf("语言未保存或应用：选择=%q 配置=%q", selected, a.cfg.Get().Language)
	}
}

func TestSettingsLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, sub := range []int{0, 1, 2} {
			t.Run(lang+"/"+strconv.Itoa(sub), func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				a.tab = 1
				a.settingsTab = sub
				tt := ui.NewTester(a.view, 780, 2600)
				tt.SetLocale(lang)
				checkPageLanguage(t, tt, a, lang)
			})
		}
	}
}
