/**
 * [INPUT]: 依赖界面文本、白名单和注入的用户数据。
 * [OUTPUT]: 提供中英混排检查 langViolations 与用户数据剔除 stripUserData，以及它们的表驱动测试。
 * [POS]: 根模块的文案验收工具。页面级语言测试先剔除用户数据，再用它判断界面文案是否混入另一种语言。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"regexp"
	"strings"
	"testing"
	"unicode"
)

var latinWord = regexp.MustCompile(`[A-Za-z]{2,}`)

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
