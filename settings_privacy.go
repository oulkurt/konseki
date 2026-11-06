/*
 * [INPUT]: 依赖采集配置、语言目录与输入控件。
 * [OUTPUT]: 提供文字识别、输入监听和敏感应用设置。
 * [POS]: 记录设置的隐私边界，将输入归一化后交给采集器。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"strconv"
	"strings"

	"konseki/internal/config"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func (a *app) updateRecorder(fn func(*config.Recorder)) {
	if err := a.cfg.Update(func(cfg *config.Config) { fn(&cfg.Recorder) }); err != nil {
		a.err = err.Error()
		return
	}
	a.applyRecorder()
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
			a.settingsRow(c, msg.RecorderInput(), msg.RecorderInputDescription(), pal, func() {
				on := a.cfgNow.Recorder.InputMonitor
				if renderPillSwitch(c, "recorder-input", &on, pal) {
					a.updateRecorder(func(r *config.Recorder) { r.InputMonitor = on })
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
		})
	})
	a.renderRecorderSummary(c, pal)
	a.settingsCard(c, pal, func() {
		ui.Column(c).FillWidth().Padding(14, 18).Gap(7).Children(func() {
			ui.Text(c, msg.RecorderSensitive()).Bold().TextColor(pal.textPrimary)
			ui.Text(c, msg.RecorderSensitiveHelp()).FontSize(12).TextColor(pal.textSecondary)
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
			a.settingsRow(c, msg.ModelsAuto(), "", pal, func() {
				on := a.cfgNow.Summary.Auto
				if renderPillSwitch(c, "recorder-summary-auto", &on, pal) {
					a.cfg.Update(func(cfg *config.Config) { cfg.Summary.Auto = on })
					a.cfgNow = a.cfg.Get()
				}
			})
		})
	})
}
