/*
 * [INPUT]: 依赖采集配置、运行中应用列表、语言目录、输入控件与模型页的自动摘要开关行。
 * [OUTPUT]: 提供文字识别、键鼠计数和敏感应用设置，敏感应用可从运行中的应用里添加。
 * [POS]: 记录设置的隐私边界，将输入归一化后交给采集器。键鼠计数开关即时更新采集设置。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"strconv"
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) updateRecorder(fn func(*config.Recorder)) {
	if err := a.cfg.Update(func(cfg *config.Config) { fn(&cfg.Recorder) }); err != nil {
		a.err = err
		return
	}
	a.applyRecorder()
}

// runningAppsCache 让菜单打开期间复用同一份应用列表。
// 菜单每帧重建，选中那一帧的序号要和打开时的列表对得上。
type runningAppsCache struct {
	at   time.Time
	apps []capture.Front
}

// candidateSensitiveApps 列出可加入的应用：运行中的应用和当前前台应用，去掉已在列表里的。
func (a *app) candidateSensitiveApps() []capture.Front {
	seen := map[string]bool{}
	for _, b := range a.cfgNow.Recorder.SensitiveApps {
		seen[strings.ToLower(strings.TrimSpace(b))] = true
	}
	var out []capture.Front
	add := func(f capture.Front) {
		b := strings.TrimSpace(f.Bundle)
		if b == "" || seen[strings.ToLower(b)] {
			return
		}
		seen[strings.ToLower(b)] = true
		out = append(out, capture.Front{App: f.App, Bundle: b})
	}
	if a.runningApps != nil {
		now := a.currentTime()
		if a.running.at.IsZero() || now.Sub(a.running.at) > 2*time.Second || now.Before(a.running.at) {
			a.running = runningAppsCache{at: now, apps: a.runningApps()}
		}
		for _, f := range a.running.apps {
			add(f)
		}
	}
	add(a.status.Front)
	return out
}

// addSensitiveApp 追加一个 bundle ID。配置归一化会按不分大小写去重。
func (a *app) addSensitiveApp(bundle string) {
	a.updateRecorder(func(r *config.Recorder) {
		r.SensitiveApps = append(r.SensitiveApps, bundle)
	})
	a.sensitiveDraft = strings.Join(a.cfgNow.Recorder.SensitiveApps, "\n")
}

func (a *app) renderRecorderPrivacy(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			a.settingsRow(c, msg.RecorderOCR(), msg.RecorderOCRDescription(), pal, func() {
				on := a.cfgNow.Recorder.OCR.Enabled
				if renderPillSwitch(c, "recorder-ocr", &on, pal) {
					a.updateRecorder(func(r *config.Recorder) { r.OCR.Enabled = on })
				}
			})
			a.settingsRow(c, msg.RecorderOCRLanguages(), msg.RecorderOCRLanguagesHelp(), pal, func() {
				if !a.ocrLanguagesFocus {
					a.ocrLanguagesDraft = strings.Join(a.cfgNow.Recorder.OCR.Languages, ", ")
				}
				in := ui.TextInput(c.Key("recorder-ocr-languages"), &a.ocrLanguagesDraft).Width(240).Label(msg.RecorderOCRLanguages())
				focused := in.Focused()
				if in.Submitted() || (a.ocrLanguagesFocus && !focused) {
					values := strings.Split(a.ocrLanguagesDraft, ",")
					a.updateRecorder(func(r *config.Recorder) { r.OCR.Languages = values })
				}
				a.ocrLanguagesFocus = focused
			})
			a.settingsRow(c, msg.RecorderOCREvery(), msg.RecorderOCREveryHelp(), pal, func() {
				if !a.ocrEveryFocus {
					a.ocrEveryDraft = strconv.Itoa(a.cfgNow.Recorder.OCR.EveryN)
				}
				in := ui.TextInput(c.Key("recorder-ocr-every"), &a.ocrEveryDraft).Width(110).Label(msg.RecorderOCREvery())
				focused := in.Focused()
				if in.Submitted() || (a.ocrEveryFocus && !focused) {
					if n, err := strconv.Atoi(strings.TrimSpace(a.ocrEveryDraft)); err == nil {
						a.updateRecorder(func(r *config.Recorder) { r.OCR.EveryN = n })
					}
				}
				a.ocrEveryFocus = focused
			})
			a.settingsRow(c, msg.RecorderInputCounts(), msg.RecorderInputCountsDescription(), pal, func() {
				on := a.cfgNow.Recorder.InputCounts
				if renderPillSwitch(c, "recorder-input-counts", &on, pal) {
					a.updateRecorder(func(r *config.Recorder) { r.InputCounts = on })
				}
			})
		})
	})
	a.renderRecorderSummary(c, pal)
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(7).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(3).Children(func() {
					headingText(c, msg.RecorderSensitive()).SingleLine().TextColor(pal.textPrimary)
					ui.Text(c, msg.RecorderSensitiveHelp()).FontSize(12).TextColor(pal.textSecondary)
				})
				renderSelectPill(c, "recorder-sensitive-add", msg.RecorderSensitiveAdd(), 145, pal.textPrimary, pal, func(m *ui.Menu) {
					candidates := a.candidateSensitiveApps()
					if len(candidates) == 0 {
						m.Item(msg.RecorderSensitiveEmpty()).Disabled(true)
						return
					}
					for _, cand := range candidates {
						label := cand.Bundle
						if cand.App != "" {
							label = cand.App + " · " + cand.Bundle
						}
						if m.Item(label).Chosen() {
							a.addSensitiveApp(cand.Bundle)
						}
					}
				})
			})
			if !a.sensitiveFocus {
				a.sensitiveDraft = strings.Join(a.cfgNow.Recorder.SensitiveApps, "\n")
			}
			in := ui.TextArea(c.Key("recorder-sensitive"), &a.sensitiveDraft).FillWidth().Height(160).Label(msg.RecorderSensitive())
			focused := in.Focused()
			if a.sensitiveFocus && !focused {
				values := strings.Split(a.sensitiveDraft, "\n")
				a.updateRecorder(func(r *config.Recorder) { r.SensitiveApps = values })
			}
			a.sensitiveFocus = focused
		})
	})
}

func (a *app) renderRecorderSummary(c *ui.Context, pal palette) {
	msg := locales.In(c.Locale())
	model := a.cfgNow.Summary.Model
	if model == "" {
		model = msg.RecorderNoModel()
	}
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Children(func() {
			a.settingsRow(c, msg.ModelsSummary(), model, pal, func() {
				if ui.Button(c.Key("recorder-model-settings"), msg.RecorderModelSettings()).Clicked() {
					a.settingsTab = 1
					a.scrollState.Y = 0
				}
			})
			a.renderAutoSummaryRow(c, pal, "recorder-summary-auto")
		})
	})
}
