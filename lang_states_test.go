/*
 * [INPUT]: 依赖离屏界面、双语目录、模拟应用状态和 lang_test.go 的 checkPageLanguage。
 * [OUTPUT]: 验证菜单、设置状态和错误说明的语言边界。
 * [POS]: 页面语言门禁；夹具中的用户数据在检查前剔除。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"errors"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/providers"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func TestRecorderStateLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, phase := range []struct {
			name  string
			value capture.Phase
			label func(locales.Messages) string
		}{
			{"off", capture.PhaseOff, locales.Messages.ReceiptOff},
			{"active", capture.PhaseActive, locales.Messages.ReceiptActive},
			{"paused", capture.PhasePaused, locales.Messages.ReceiptPaused},
			{"waiting", capture.PhaseWaiting, locales.Messages.ReceiptWaiting},
			{"permission", capture.PhaseBlocked, locales.Messages.ReceiptBlocked},
		} {
			for _, tab := range []int{0, 1} {
				t.Run(lang+"/"+phase.name+"/"+string(rune('0'+tab)), func(t *testing.T) {
					a, cleanup := newTestApp(t)
					defer cleanup()
					a.tab, a.settingsTab = tab, 0
					a.status.Phase, a.status.Block = phase.value, capture.BlockNoScreenPermission
					a.lastLoad = time.Now().Add(time.Hour)
					tt := ui.NewTester(a.view, 780, 2800)
					tt.SetLocale(lang)
					if !tt.HasText(phase.label(locales.In(lang))) {
						t.Fatalf("状态没有渲染: %q", tt.Texts())
					}
					checkPageLanguage(t, tt, a, lang)
				})
			}
		}
	}
}

func TestDynamicSettingsLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		for _, state := range []string{"confirmation", "clearing", "permission-granted", "permission-missing", "sessions", "agent-legacy", "agent-errors", "provider-edit", "provider-search", "import", "scanning", "read-error", "clear-error", "provider-result"} {
			t.Run(lang+"/"+state, func(t *testing.T) {
				a, cleanup := newTestApp(t)
				defer cleanup()
				a.tab = 1
				a.lastLoad = time.Now().Add(time.Hour)
				user := []string{"外部错误 external error", "用户标题 User title", "本机凭据 local credential", "用户供应商 User provider", "用户模型 User model"}
				msg := locales.In(lang)
				want := ""
				switch state {
				case "read-error":
					a.err = errors.New(user[0])
					want = msg.ShellReadError() + errorText(lang, a.err)
				case "clear-error":
					a.clearErr = clearCountError(2)
					want = msg.RecorderDeleteFailed(2)
				case "confirmation":
					a.clearConfirm = true
					want = msg.RecorderClearTitle()
				case "clearing":
					a.clearRunning = true
					want = msg.RecorderClearing()
				case "permission-granted":
					a.status.Perms = capture.Perms{Screen: capture.PermGranted, Accessibility: capture.PermGranted}
					want = msg.RecorderPermissionGranted()
				case "permission-missing":
					a.status.Perms = capture.Perms{Screen: capture.PermDenied, Accessibility: capture.PermDenied}
					want = msg.RecorderPermissionMissing()
				case "sessions":
					if err := a.cfg.Update(func(c *config.Config) { c.Recorder.Enabled = true }); err != nil {
						t.Fatal(err)
					}
					a.cfgNow = a.cfg.Get()
					a.lastSessions = time.Now().Add(time.Hour)
					at := time.Now()
					a.recentSessions = []recentSession{{session: capture.Session{ID: "sample", Start: at.Add(-time.Minute), End: at, Snapshots: 2}, title: user[1]}, {session: capture.Session{ID: "fallback", Start: at.Add(-time.Minute), End: at}}}
					want = msg.RecorderRecent()
				case "agent-legacy", "agent-errors":
					a.settingsTab = 2
					a.legacyChecked = true
					if state == "agent-legacy" {
						a.legacyFiles = []string{"legacy"}
						want = msg.GeneralAgentLegacy()
					} else {
						a.linkErr = errors.New(user[0])
						a.legacyErr = errors.New(user[0])
						want = msg.GeneralAgentLinkError(errorText(lang, a.linkErr))
					}
				case "provider-edit", "provider-search", "import", "scanning", "provider-result":
					a.settingsTab = 1
					a.detectStarted, a.detectDone = true, true
					switch state {
					case "provider-edit", "provider-result":
						pc := config.ProviderConfig{Preset: "custom", Label: user[3], DefaultModel: user[4]}
						if err := a.cfg.Update(func(c *config.Config) { c.Providers = []config.ProviderConfig{pc} }); err != nil {
							t.Fatal(err)
						}
						a.cfgNow = a.cfg.Get()
						a.openProvider = providerKey(pc)
						want = msg.ModelsKey()
						if state == "provider-result" {
							a.testResult = msg.ModelsTestSuccess(user[4], 12)
							want = a.testResult
						}
					case "provider-search":
						a.addProviderOpen = true
						a.providerSearch = "openai"
						want = msg.ModelsSearch()
						for _, p := range providers.All() {
							user = append(user, p.Name)
						}
					case "import":
						a.detected = []providers.Credential{{Source: "test", Label: user[2]}}
						want = msg.ModelsImportAction()
					case "scanning":
						a.detectDone = false
						want = msg.ModelsScanning()
					}
				}
				tt := ui.NewTester(a.view, 780, 3000)
				tt.SetLocale(lang)
				if !tt.HasText(want) {
					t.Fatalf("状态 %s 没有渲染 %q: %q", state, want, tt.Texts())
				}
				checkPageLanguage(t, tt, a, lang, user...)
			})
		}
	}
}

func TestReceiptDateMenuLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			tt := ui.NewTester(a.view, 780, 1000)
			tt.SetLocale(lang)
			msg := locales.In(lang)
			if err := tt.Click(msg.ReceiptToday() + " ⌄"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText(msg.ReceiptYesterday()) {
				t.Fatalf("日期菜单未打开: %q", tt.Texts())
			}
			checkPageLanguage(t, tt, a, lang)
		})
	}
}

func TestShredLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		t.Run(lang, func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			a.lastLoad = time.Now().Add(time.Hour)
			a.clearReceipt = a.receipt
			a.shred.pending = true
			tt := ui.NewTester(a.view, 780, 1000)
			tt.SetLocale(lang)
			for _, duration := range []time.Duration{17 * time.Millisecond, 650 * time.Millisecond, 100 * time.Millisecond} {
				tt.Advance(duration)
				if !a.shred.pending {
					t.Fatal("碎纸过早结束")
				}
				checkPageLanguage(t, tt, a, lang)
			}
			if !a.shred.feedStarted || a.shred.mask == nil {
				t.Fatal("未进入碎纸阶段")
			}
		})
	}
}
