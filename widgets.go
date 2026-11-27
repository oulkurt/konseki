/*
 * [INPUT]: 依赖 MyGo 界面基础组件与玻璃材质。
 * [OUTPUT]: 提供配色和设置控件。
 * [POS]: 根模块的共享视图构件，供设置页和导航使用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

type palette struct {
	canvasBg      ui.Color
	cardBg        ui.Color
	cardBorder    ui.Color
	rowDivider    ui.Color
	controlBorder ui.Color
	textPrimary   ui.Color
	textSecondary ui.Color
	textSection   ui.Color
	textTime      ui.Color
	tickMajor     ui.Color
	tickMinor     ui.Color
	rowHoverBg    ui.Color
	rowExpandedBg ui.Color
	codeBg        ui.Color
	codeFg        ui.Color
	bulletColor   ui.Color
	switchOn      ui.Color
	switchOff     ui.Color
}

func themePalette(c *ui.Context) palette {
	t := c.Theme()
	isDark := t.Background.R < 80 && t.Background.G < 80 && t.Background.B < 80
	if isDark {
		return palette{
			canvasBg:      ui.Hex("#0F1115"),
			cardBg:        ui.Hex("#16181E"),
			cardBorder:    ui.RGBA(255, 255, 255, 0.09),
			rowDivider:    ui.RGBA(255, 255, 255, 0.06),
			controlBorder: ui.RGBA(255, 255, 255, 0.14),
			textPrimary:   ui.Hex("#F3F4F6"),
			textSecondary: ui.Hex("#9CA3AF"),
			textSection:   ui.Hex("#D1D5DB"),
			textTime:      ui.Hex("#9CA3AF"),
			tickMajor:     ui.Hex("#E5E7EB"),
			tickMinor:     ui.RGBA(156, 163, 175, 0.28),
			rowHoverBg:    ui.RGBA(255, 255, 255, 0.055),
			rowExpandedBg: ui.RGBA(255, 255, 255, 0.03),
			codeBg:        ui.RGBA(255, 255, 255, 0.11),
			codeFg:        ui.Hex("#E5E7EB"),
			bulletColor:   ui.Hex("#9CA3AF"),
			switchOn:      ui.Hex("#15B76C"),
			switchOff:     ui.Hex("#3F3F46"),
		}
	}
	return palette{
		canvasBg:      ui.Hex("#F7F8FA"),
		cardBg:        ui.Hex("#FFFFFF"),
		cardBorder:    ui.Hex("#E6E8EC"),
		rowDivider:    ui.Hex("#EFEFEF"),
		controlBorder: ui.Hex("#E2E4E9"),
		textPrimary:   ui.Hex("#111827"),
		textSecondary: ui.Hex("#6B7280"),
		textSection:   ui.Hex("#4B5563"),
		textTime:      ui.Hex("#6B7280"),
		tickMajor:     ui.Hex("#1F2937"),
		tickMinor:     ui.RGBA(156, 163, 175, 0.52),
		rowHoverBg:    ui.RGBA(243, 244, 246, 0.85),
		rowExpandedBg: ui.RGBA(249, 250, 251, 0.65),
		codeBg:        ui.RGBA(15, 23, 42, 0.065),
		codeFg:        ui.Hex("#1E293B"),
		bulletColor:   ui.Hex("#6B7280"),
		switchOn:      ui.Hex("#15B76C"),
		switchOff:     ui.Hex("#E4E4E7"),
	}
}

func (a *app) settingsCard(c *ui.Context, pal palette, children func()) {
	ui.Column(c).
		FillWidth().
		Radius(14).
		Background(pal.cardBg).
		Border(1, pal.cardBorder).
		Shadow(0, 2, 10, -2, ui.RGBA(15, 23, 42, 0.03)).
		Clip().
		Children(children)
}

func (a *app) settingsRow(c *ui.Context, title, subtitle string, pal palette, rightControl func()) {
	ui.Row(c).
		FillWidth().
		Padding(14, 18).
		AlignItems(ui.Center).
		Gap(16).
		Children(func() {
			ui.Column(c).Grow(1).Gap(3).Children(func() {
				headingText(c, title).
					FontSize(13.5).
					SingleLine().
					TextColor(pal.textPrimary)
				// 空文字也占一行高，标题会被顶到右侧控件的上方。
				if subtitle != "" {
					ui.Text(c, subtitle).
						FontSize(12.2).
						TextColor(pal.textSecondary).
						LineHeight(1.38)
				}
			})
			rightControl()
		})
}

// renderPillSwitch 绘制与设计稿一致的翡翠绿胶囊开关（带加宽白色胶囊滑块）
func renderPillSwitch(c *ui.Context, key string, val *bool, pal palette) bool {
	sw := ui.SwitchBase(c.Key(key), val)
	sw.Size(44, 24).Radius(12).Cursor(ui.CursorPointer)

	trackColor := pal.switchOff
	targetX := float32(2.5)
	if *val {
		trackColor = pal.switchOn
		targetX = 19.5
	}
	thumbX := sw.Animate("thumb-x", targetX, 150*time.Millisecond)

	sw.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(r, trackColor, 12)
		thumbRect := ui.Rect{
			X: r.X + thumbX,
			Y: r.Y + 2.5,
			W: 22,
			H: 19,
		}
		p.Shadow(thumbRect, 9.5, 0, 1, 2.5, 0, ui.RGBA(0, 0, 0, 0.15))
		p.Fill(thumbRect, ui.Hex("#FFFFFF"), 9.5)
	})

	return sw.Changed()
}

// renderSelectPill 绘制带上下箭头的下拉选择按钮（如 Select ⌃⌄、14 days ⌃⌄）
func renderSelectPill(c *ui.Context, key, label string, minW float32, textColor ui.Color, pal palette, menu func(*ui.Menu)) {
	btn := ui.ButtonBase(c.Key(key)).
		MinWidth(minW).
		Height(32).
		Padding(0, 12).
		Gap(8).
		Radius(8).
		Background(pal.cardBg).
		Border(1, pal.controlBorder).
		Material(glass.Glass{Style: glass.Clear, Interactive: true, Clarity: 0.7}).
		PopoverMenuButton(menu)
	if btn.Hovered() {
		btn.Background(pal.rowHoverBg)
	}
	btn.Children(func() {
		ui.Text(c, label).
			FontSize(12.5).
			Bold().
			TextColor(textColor).
			Grow(1).
			SingleLine().
			Tooltip(label)
		ui.Icon(c, chevronsUpDownSVG).FontSize(12.5).TextColor(pal.textSecondary)
	})
}
