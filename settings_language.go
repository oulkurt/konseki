/*
 * [INPUT]: 依赖语言目录、设置存储与 MyGo 语言名称。
 * [OUTPUT]: 提供语言切换行和持久化操作。
 * [POS]: 通用设置页的语言入口。语言同时决定界面和摘要提示词。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

func (a *app) setLanguage(lang string) {
	if err := a.cfg.Update(func(c *config.Config) { c.Language = lang }); err != nil {
		a.err = err
		return
	}
	a.cfgNow = a.cfg.Get()
	a.setLocale(lang)
	a.rebuildGenerator()
}

func (a *app) renderLanguageSection(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		a.settingsRow(c, msg.TabsLanguage(), msg.TabsLanguageHelp(), pal, func() {
			menu := func(m *ui.Menu) {
				for _, lang := range []string{"zh-Hans", "en"} {
					if m.Item(i18n.Autonym(lang)).Checked(a.cfgNow.Language == lang).Chosen() {
						a.setLanguage(lang)
					}
				}
			}
			renderSelectPill(c, "language", i18n.Autonym(a.cfgNow.Language), 110, pal.textPrimary, pal, menu)
		})
	})
}
