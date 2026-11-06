/*
 * [INPUT]: 依赖摘要任务、采集清除操作与 Skill 导出目录。
 * [OUTPUT]: 提供按顺序执行的后台清除流程。
 * [POS]: 设置页危险操作的边界；完成状态只在界面线程发布。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"os"
	"time"

	"konseki/internal/skill"
	"konseki/locales"
)

// clearSequence 等待摘要退出，再依次清除采集、摘要和导出。
func clearSequence(done <-chan struct{}, captureClear, summaryClear, reconcile func() error) error {
	if done != nil {
		<-done
	}
	for _, step := range []func() error{captureClear, summaryClear, reconcile} {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) clearActivity() {
	if a.clearRunning || a.runner == nil || a.sums == nil {
		return
	}
	a.clearRunning = true
	a.clearReceipt = a.receipt
	a.clearErr = ""
	a.sumEpoch++
	if a.sumCancel != nil {
		a.sumCancel()
	}
	done := a.sumDone
	runner, sums := a.runner, a.sums
	opts := skill.Options{Dir: a.cfgNow.Skill.ExportDir, Days: a.cfgNow.Skill.Days, IncludeOCR: a.cfgNow.Skill.IncludeOCR, Owner: ownerName()}
	post := a.post
	lang := a.cfgNow.Language
	go func() {
		err := clearSequence(done, func() error {
			cleared, err := runner.ClearAll()
			if err != nil {
				return err
			}
			if cleared.Failed != 0 {
				return errors.New(locales.In(lang).RecorderDeleteFailed(cleared.Failed))
			}
			return nil
		}, sums.Clear, func() error {
			dir := opts.Dir
			if dir == "" {
				dir = skill.DefaultDir()
			}
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			_, err := skill.Reconcile(opts, nil)
			return err
		})
		post(func() {
			a.clearRunning = false
			a.sumRunning = false
			a.sumCancel = nil
			a.sumDone = nil
			if err != nil {
				a.clearErr = err.Error()
				return
			}
			a.sumEntries = nil
			a.blocks = nil
			a.receiptStore = nil
			a.receipt = ReceiptDay{}
			a.status = runner.Status()
			a.lastLoad = time.Time{}
		})
	}()
}
