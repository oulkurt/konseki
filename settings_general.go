/*
 * [INPUT]: 依赖技能导出配置、导出服务和语言目录。
 * [OUTPUT]: 提供技能导出设置与操作。
 * [POS]: 通用设置页的导出边界，持久化目录与隐私选项。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"time"

	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) renderSkillSection(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(12).Children(func() {
			ui.Text(c, msg.GeneralSkill()).Bold().TextColor(pal.textPrimary)
			ui.Text(c, msg.GeneralSkillDescription()).FontSize(12).TextColor(pal.textSecondary)
			ui.Text(c, msg.GeneralSkillDirectory()).FontSize(12).Bold().TextColor(pal.textPrimary)
			if !a.skillDirFocus {
				a.skillDirDraft = a.cfgNow.Skill.ExportDir
			}
			placeholder := a.cfgNow.Skill.ExportDir
			if placeholder == "" {
				placeholder = a.skillDefaultDirectory()
			}
			in := ui.TextInput(c.Key("skill-dir"), &a.skillDirDraft).FillWidth().Label(msg.GeneralSkillDirectory()).Placeholder(placeholder)
			focused := in.Focused()
			if in.Submitted() || (a.skillDirFocus && !focused) {
				if err := a.cfg.Update(func(cfg *config.Config) { cfg.Skill.ExportDir = a.skillDirDraft }); err != nil {
					a.err = err.Error()
				}
				a.cfgNow = a.cfg.Get()
			}
			a.skillDirFocus = focused
			if ui.Button(c.Key("btn-skill-export"), msg.GeneralSkillExport()).Disabled(a.clearRunning).Clicked() {
				if n, err := a.exportSkill(); err != nil {
					a.skillExportMsg = msg.GeneralSkillError(err.Error())
				} else {
					a.skillExportMsg = msg.GeneralSkillExported(n)
					a.skillExportedAt = time.Now()
				}
			}
			if a.skillExportMsg != "" {
				ui.Text(c, a.skillExportMsg).FontSize(12).TextColor(pal.textSecondary)
			}
		})
	})
}
