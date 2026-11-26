/*
 * [INPUT]: 依赖语言目录、旧导出文件、命令链接和剪贴板服务。
 * [OUTPUT]: 提供通用页的 Agent 技能卡片：话术、安装命令、仓库和旧文件清理。
 * [POS]: 通用设置页的说明边界。三处复制按钮写剪贴板，两秒后恢复文字。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"konseki/internal/query"
	"konseki/internal/skill"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

const (
	agentSkillRepo    = "https://github.com/oulkurt/konseki/tree/main/skills/konseki"
	agentSkillInstall = "npx skills add oulkurt/konseki --skill konseki -g -y"
)

func statusCmd(appDir string) string {
	return "KONSEKI_HOME=" + shellQuote(appDir) + " " + shellQuote(filepath.Join(appDir, query.LinkName)) + " query status"
}

func shellQuote(path string) string {
	escape := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`").Replace(s)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return `"$HOME/` + escape(filepath.ToSlash(rel)) + `"`
		}
	}
	return `"` + escape(path) + `"`
}

func (a *app) renderAgentSection(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	prompt := msg.GeneralAgentPromptText(agentSkillInstall, statusCmd(a.appDir))
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			ui.Text(c, msg.GeneralAgent()).Padding(14, 18, 4, 18).Bold().TextColor(pal.textPrimary)
			ui.Text(c, msg.GeneralAgentDescription()).Padding(0, 18, 0, 18).FontSize(12).TextColor(pal.textSecondary).LineHeight(1.4)

			a.settingsRow(c, msg.GeneralAgentPrompt(), msg.GeneralAgentPromptHelp(), pal, func() {
				a.copyButton(c, msg, "btn-agent-copy-prompt", prompt)
			})
			a.agentTextBox(c, pal, prompt, 12, false)

			a.settingsRow(c, msg.GeneralAgentInstall(), msg.GeneralAgentInstallHelp(), pal, func() {
				a.copyButton(c, msg, "btn-agent-copy-install", agentSkillInstall)
			})
			a.agentTextBox(c, pal, agentSkillInstall, 11.5, true)

			a.settingsRow(c, msg.GeneralAgentRepo(), agentSkillRepo, pal, func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.Button(c.Key("btn-agent-open-repo"), msg.GeneralAgentOpen()).Clicked() {
						if a.openExternal != nil {
							if err := a.openExternal(agentSkillRepo); err != nil {
								a.err = err
							}
						}
					}
					a.copyButton(c, msg, "btn-agent-copy-repo", agentSkillRepo)
				})
			})

			ui.Text(c, msg.GeneralAgentRedact()).Padding(0, 18, 14, 18).FontSize(12).TextColor(pal.textSecondary)
			if a.linkErr != nil {
				ui.Text(c, msg.GeneralAgentLinkError(errorText(c.Locale(), a.linkErr))).FontSize(12).TextColor(c.Theme().Danger)
			}
			a.renderLegacyRow(c, pal, msg)
		})
	})
}

func (a *app) agentTextBox(c *ui.Context, pal palette, text string, size float32, mono bool) {
	ui.Column(c).FillWidth().Padding(0, 18, 14, 18).Children(func() {
		el := ui.Text(c, text).
			FillWidth().
			Padding(10, 12).
			Radius(8).
			Background(pal.rowHoverBg).
			FontSize(size).
			LineHeight(1.45).
			TextColor(pal.textPrimary).
			Selectable()
		if mono {
			el.Font("monospace")
		}
	})
}

func (a *app) copyButton(c *ui.Context, msg locales.Messages, key, text string) {
	label := msg.GeneralAgentCopy()
	if a.agentCopiedKey == key {
		if left := a.agentCopiedAt.Add(2 * time.Second).Sub(c.Now()); left > 0 {
			label = msg.GeneralAgentCopied()
			c.After(left)
		} else {
			a.agentCopiedKey = ""
		}
	}
	if ui.Button(c.Key(key), label).Clicked() {
		c.Services().WriteClipboard(text)
		a.agentCopiedKey = key
		a.agentCopiedAt = c.Now()
		c.After(2 * time.Second)
	}
}

func (a *app) renderLegacyRow(c *ui.Context, pal palette, msg locales.Messages) {
	if a.legacy.phase == legacyUnchecked {
		a.checkLegacy(msg)
	}
	if a.legacy.err != nil {
		ui.Text(c, msg.GeneralAgentLegacyError(errorText(c.Locale(), a.legacy.err))).FontSize(12).TextColor(c.Theme().Danger)
	}
	if len(a.legacy.files) == 0 {
		return
	}
	dir := a.legacyDir()
	a.settingsRow(c, msg.GeneralAgentLegacy(), msg.GeneralAgentLegacyHelp(len(a.legacy.files), dir), pal, func() {
		btn := ui.Button(c.Key("btn-agent-remove-legacy"), msg.GeneralAgentLegacyRemove()).
			Disabled(a.clear.running()).
			TextColor(c.Theme().Danger).
			Border(1, c.Theme().Danger)
		if btn.Clicked() {
			var removeErr error
			if _, err := skill.RemoveLegacy(dir); err != nil {
				removeErr = err
			}
			a.checkLegacy(msg)
			if removeErr != nil {
				a.legacy.phase = legacyFailed
				a.legacy.err = removeErr
			}
		}
	})
}

func (a *app) checkLegacy(msg locales.Messages) {
	files, err := skill.FindLegacy(a.legacyDir())
	if err != nil {
		a.legacy.phase = legacyFailed
		a.legacy.files = nil
		a.legacy.err = err
		return
	}
	a.legacy.phase = legacyReady
	a.legacy.files = files
	a.legacy.err = nil
}
