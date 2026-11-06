/*
 * [INPUT]: 依赖测试应用、离屏渲染器、旧导出样本和剪贴板。
 * [OUTPUT]: 验证 Agent 卡片的复制、恢复、打开仓库、旧文件删除行和语言切换。
 * [POS]: 通用页 Agent 技能卡片的界面回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"konseki/internal/skill"

	"github.com/egoist/mygo/ui"
)

type agentLabels struct {
	copy, copied, open, legacy, remove string
}

func labelsFor(lang string) agentLabels {
	if lang == "en" {
		return agentLabels{"Copy", "Copied", "Open", "Old export", "Delete old files"}
	}
	return agentLabels{"复制", "已复制", "打开", "旧版导出", "删除旧文件"}
}

func agentTester(t *testing.T, a *app, lang string) *ui.Tester {
	t.Helper()
	a.tab, a.settingsTab = 1, 2
	tt := ui.NewTester(a.view, 780, 1800)
	tt.SetLocale(lang)
	return tt
}

func TestAgentCardCopies(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			opened := ""
			a.openExternal = func(url string) error { opened = url; return nil }
			tt := agentTester(t, a, lang)
			labels := labelsFor(lang)

			if err := tt.Click(labels.copy); err != nil {
				t.Fatal(err)
			}
			prompt := tt.Clipboard()
			if !strings.Contains(prompt, agentSkillInstall) {
				t.Fatalf("话术里没有安装命令: %q", prompt)
			}
			if !strings.Contains(prompt, statusCmd(a.appDir)) {
				t.Fatalf("话术里没有查询命令: %q", prompt)
			}
			if !tt.HasText(labels.copied) {
				t.Fatal("复制后按钮文字没有变成已复制")
			}
			if !strings.Contains(statusCmd(a.appDir), filepath.Join(a.appDir, "konseki")) {
				t.Fatalf("查询命令缺少链接路径: %q", statusCmd(a.appDir))
			}

			tt.Advance(2 * time.Second)
			if tt.HasText(labels.copied) {
				t.Fatal("两秒后按钮文字没有恢复")
			}

			if err := tt.Click(labels.copy); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if got := tt.Clipboard(); got != prompt {
				t.Fatalf("恢复后复制的话术不一致: %q", got)
			}
			if err := tt.Click(labels.copy); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if got := tt.Clipboard(); got != agentSkillInstall {
				t.Fatalf("安装命令剪贴板 = %q", got)
			}
			// 同一时间只有一个按钮显示「已复制」，文本查找只能找到第一个「复制」。
			// 仓库的复制按钮用「打开」的位置往右偏移点击。
			openRect, ok := tt.Find(labels.open)
			if !ok {
				t.Fatal("找不到打开按钮")
			}
			tt.ClickAt(openRect.X+openRect.W+35, openRect.Y+openRect.H/2)
			tt.Frame()
			if got := tt.Clipboard(); got != agentSkillRepo {
				t.Fatalf("仓库剪贴板 = %q", got)
			}

			if err := tt.Click(labels.open); err != nil {
				t.Fatal(err)
			}
			if opened != agentSkillRepo {
				t.Fatalf("打开的网址 = %q", opened)
			}
		})
	}
}

func copyLegacyFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("internal", "skill", "testdata", "legacy"))); err != nil {
		t.Fatal(err)
	}
}

func TestAgentCardLegacyRow(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			tt := agentTester(t, a, lang)
			labels := labelsFor(lang)
			if tt.HasText(labels.legacy) {
				t.Fatal("没有旧文件时出现了删除行")
			}

			dir := a.legacyDir()
			copyLegacyFixture(t, dir)
			a.resetLegacy()
			tt.Frame()
			if !tt.HasText(labels.legacy) || !tt.HasText(labels.remove) {
				t.Fatalf("旧文件存在时缺少删除行: %q", tt.Texts())
			}

			if err := tt.Click(labels.remove); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if files, err := skill.FindLegacy(dir); err != nil || len(files) != 0 {
				t.Fatalf("旧文件未删除: %v %v", files, err)
			}
			if tt.HasText(labels.legacy) {
				t.Fatalf("删除后仍显示删除行: %q", tt.Texts())
			}
		})
	}
}
