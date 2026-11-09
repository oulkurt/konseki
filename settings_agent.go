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

// statusCmd 是话术里的试查命令。显式传入数据目录，路径与软链接同名。
func statusCmd(appDir string) string {
	return "KONSEKI_HOME=" + shellQuote(appDir) + " " + shellQuote(filepath.Join(appDir, query.LinkName)) + " query status"
}

// shellQuote 用双引号包住路径，让整条命令可以直接粘进终端。
// 家目录下的路径写成 $HOME，其余部分的引号、美元符、反引号和反斜杠转义。
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

// agentTextBox 是可选中的等宽或正文框，背景和行高与设置行区分开。
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

// copyButton 写剪贴板，并让按钮文字在两秒内显示「已复制」。
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

// renderLegacyRow 在发现旧导出文件时给出删除行。
// 检查只做一次并缓存；删除成功或清除全部数据后重做。
func (a *app) renderLegacyRow(c *ui.Context, pal palette, msg locales.Messages) {
	if !a.legacyChecked {
		a.checkLegacy(msg)
	}
	if a.legacyErr != nil {
		ui.Text(c, msg.GeneralAgentLegacyError(errorText(c.Locale(), a.legacyErr))).FontSize(12).TextColor(c.Theme().Danger)
	}
	if len(a.legacyFiles) == 0 {
		return
	}
	dir := a.legacyDir()
	a.settingsRow(c, msg.GeneralAgentLegacy(), msg.GeneralAgentLegacyHelp(len(a.legacyFiles), dir), pal, func() {
		btn := ui.Button(c.Key("btn-agent-remove-legacy"), msg.GeneralAgentLegacyRemove()).
			TextColor(c.Theme().Danger).
			Border(1, c.Theme().Danger)
		if btn.Clicked() {
			if _, err := skill.RemoveLegacy(dir); err != nil {
				a.legacyErr = err
			} else {
				a.legacyErr = nil
			}
			a.checkLegacy(msg)
		}
	})
}

func (a *app) checkLegacy(msg locales.Messages) {
	a.legacyChecked = true
	files, err := skill.FindLegacy(a.legacyDir())
	if err != nil {
		a.legacyFiles = nil
		a.legacyErr = err
		return
	}
	a.legacyFiles = files
}
