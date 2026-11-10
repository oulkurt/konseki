/*
 * [INPUT]: 依赖 MyGo 的文本元素与字体特性。
 * [OUTPUT]: 提供标题、正文与数字三类文本构件。
 * [POS]: 根模块的排版边界。设置页、小票和时间轴共用这里的字体规则，调用方只决定字号和颜色。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import "github.com/egoist/mygo/ui"

// headingText 是标题的起点：加粗，最多两行，超长以省略号收尾。
// 两行上限挡住窄容器里的孤字折行；确定只有一行的短标题再接 SingleLine。
func headingText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).Bold().MaxLines(2).Ellipsis("…")
}

// bodyText 是正文的起点：1.4 倍行高。
// 字号和颜色由调用方按层级决定，行高只在这里统一一次。
func bodyText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).LineHeight(1.4)
}

// numText 是数字的起点：等宽数字。
// 时间、计数和百分比按列刷新时，字宽固定才不会左右抖。
func numText(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontFeatures("tnum")
}
