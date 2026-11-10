/*
 * [INPUT]: 依赖排版构件和 MyGo 测试器。
 * [OUTPUT]: 验证三类文本构件的文案不丢字，链式配置不互相覆盖。
 * [POS]: 根模块排版边界的回归测试。只查可见文案，不查视觉细节。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTypographyTextBuilders 让三类构件各渲染一次。
// tnum、行高和行数上限是元素内部属性，测试器看不到；这里守住文案仍在、链式调用可用。
func TestTypographyTextBuilders(t *testing.T) {
	view := func(c *ui.Context) {
		ui.Column(c).Padding(12).Gap(4).Children(func() {
			headingText(c, "标题 Heading").FontSize(13.5)
			headingText(c, "单行标题").FontSize(12).SingleLine()
			bodyText(c, "正文说明正文说明").FontSize(12).TextColor(ui.Hex("#333333"))
			numText(c, "12 分钟 · 3 张快照").FontSize(11)
		})
	}
	tt := ui.NewTester(view, 320, 240)
	for _, want := range []string{"标题 Heading", "单行标题", "正文说明正文说明", "12 分钟 · 3 张快照"} {
		if !tt.HasText(want) {
			t.Errorf("缺少 %q，texts=%q", want, tt.Texts())
		}
	}
}
