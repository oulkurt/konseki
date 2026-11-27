/*
 * [INPUT]: 依赖摘要任务、采集清除操作与旧版技能导出文件。
 * [OUTPUT]: 提供确认界面、清除失败种类与后台清除流程。
 * [POS]: 设置页危险操作的边界；最后一步删除旧导出文件，不写任何文件。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"fmt"
	"time"

	"konseki/internal/skill"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

type clearCountError int

func (e clearCountError) Error() string { return fmt.Sprintf("%d files could not be deleted", e) }

func (a *app) renderDanger(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	usage := a.status.Usage
	hasData := usage.Bytes > 0 || usage.Shots > 0 || usage.Snapshots > 0 || usage.Sessions > 0 || a.hasSummaries
	canClear := hasData || a.clear.phase == clearFailed || len(a.legacy.files) > 0
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(10).Children(func() {
			headingText(c, msg.RecorderDanger()).SingleLine().TextColor(c.Theme().Danger)
			bodyText(c, msg.RecorderClearDescription()).FontSize(12).TextColor(pal.textSecondary)
			label := msg.RecorderClear()
			if a.clear.running() {
				label = msg.RecorderClearing()
			}
			btn := ui.Button(c.Key("clear-activity"), label).Disabled(!canClear || a.clear.running())
			if canClear && !a.clear.running() {
				btn.TextColor(c.Theme().Danger).Border(1, c.Theme().Danger)
			}
			if btn.Clicked() {
				a.clear.confirm = true
			}
			if a.clear.err != nil {
				ui.Text(c, errorText(c.Locale(), a.clear.err)).TextColor(c.Theme().Danger).FontSize(12)
			}
		})
	})
	if ui.AlertDialog(c, &a.clear.confirm, msg.RecorderClearTitle(), msg.RecorderClearDescription(), msg.RecorderCancel(), msg.RecorderConfirm()) == 1 {
		a.clearActivity()
	}
}

func clearSequence(done <-chan struct{}, captureClear, summaryClear, removeLegacy func() error) error {
	if done != nil {
		<-done
	}
	for _, step := range []func() error{captureClear, summaryClear, removeLegacy} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) clearActivity() {
	if a.clear.running() || a.runner == nil || a.sums == nil || a.tasks.isClosed() {
		return
	}
	a.clear.phase = clearRunning
	a.clear.confirm = false
	a.clear.receipt = a.receipt
	a.clear.err = nil
	done := a.summary.waitAllChannel()
	a.summary.cancelActive()
	runner, sums := a.runner, a.sums
	legacyDir := a.legacyDir()
	ok := a.tasks.goTask(func(context.Context) {
		err := clearSequence(done, func() error {
			cleared, err := runner.ClearAll()
			if err != nil {
				return err
			}
			if cleared.Failed != 0 {
				return clearCountError(cleared.Failed)
			}
			return nil
		}, sums.Clear, func() error {
			_, err := skill.RemoveLegacy(legacyDir)
			return err
		})
		a.postUI(func() {
			a.summary.cancelActive()
			a.recentSessions = nil
			a.lastSessions = time.Time{}
			a.status = runner.Status()
			a.lastLoad = time.Time{}
			a.receiptStore = nil
			a.receiptStoreDir = ""
			a.hasSummaries = len(sums.Days()) > 0
			a.resetLegacy()
			if err != nil {
				a.clear.phase = clearFailed
				a.clear.err = err
				a.clear.receipt = ReceiptDay{}
				return
			}
			a.clear.phase = clearIdle
			a.clear.err = nil
			snapshot := a.clear.receipt
			a.clear.receipt = ReceiptDay{}
			a.summary.phase = summaryIdle
			a.summary.dayKey = ""
			a.summary.err = nil
			a.summaryReadErr = nil
			a.rebuildGenerator()
			a.sumEntries = nil
			a.blocks = nil
			a.receipt = ReceiptDay{}
			a.expanded = map[string]bool{}
			a.receiptTarget = ""
			a.err = nil
			a.readErr = nil
			a.addProviderOpen = false
			a.wasAddOpen = false
			a.providerSearch = ""
			a.tab = 0
			a.dayFilter = ""
			a.scrollState.Y = 0
			a.shred.arm(snapshot)
			a.paperFeed.resetDay()
		})
	})
	if !ok {
		a.clear.phase = clearIdle
		a.clear.receipt = ReceiptDay{}
	}
}
