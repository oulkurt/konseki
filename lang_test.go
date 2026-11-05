/**
 * [INPUT]: 依赖界面文本和用户数据白名单。
 * [OUTPUT]: 提供语言混排检查及其测试。
 * [POS]: 根模块的文案验收辅助工具，供页面测试复用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var latinWords = regexp.MustCompile(`[A-Za-z]{2,}`)

// langViolations 返回不属于 lang 的文本片段。
func langViolations(texts []string, lang string, allow []string) []string {
	var out []string
	for _, s := range texts {
		for _, a := range allow {
			s = strings.ReplaceAll(s, a, "")
		}
		if lang == "zh-Hans" {
			out = append(out, latinWords.FindAllString(s, -1)...)
			continue
		}
		if lang == "en" {
			for _, r := range s {
				if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef {
					out = append(out, string(r))
				}
			}
		}
	}
	return out
}
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
func TestLangViolations(t *testing.T) {
	for _, c := range []struct {
		s, lang     string
		allow, data []string
		bad         bool
	}{{"你好", "zh-Hans", nil, nil, false}, {"今天 Today", "zh-Hans", nil, nil, true}, {"Konseki", "zh-Hans", []string{"Konseki"}, nil, false}, {"Hello", "en", nil, nil, false}, {"Today 今天", "en", nil, nil, true}, {"Hello，", "en", nil, nil, true}, {"Hello 今天", "en", nil, []string{"今天"}, false}} {
		t.Run(c.s, func(t *testing.T) {
			got := langViolations(stripUserData([]string{c.s}, c.data...), c.lang, c.allow)
			if (len(got) > 0) != c.bad {
				t.Fatalf("违规: %q", got)
			}
		})
	}
}
