/*
 * [INPUT]: 依赖根包显式状态机、模拟时钟、异步队列与 UI 测试器。
 * [OUTPUT]: 验证跨天摘要护栏、供应商连接测试隔离、切页焦点防僵尸提交、动画状态机、暂停开关与结构化关闭。
 * [POS]: 根包交错时序与状态机回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/llm"
	"konseki/internal/providers"
	"konseki/internal/summary"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

func TestSummaryIgnoresStaleRunAfterDaySwitchAndReloadsIncremental(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	reqStarted := make(chan struct{})
	releaseReq := make(chan struct{})
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		close(reqStarted)
		<-releaseReq
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"fake","choices":[{"message":{"content":"{\"headline\":\"旧日期新条目\",\"summary\":\"内容\"}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})

	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "test", ChatBase: "https://example.test/v1", DefaultModel: "fake"}}
	}); err != nil {
		t.Fatal(err)
	}
	a.rebuildGenerator()

	oldDay := "2026-11-04"
	newDay := "2026-11-05"
	oldStart := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	if err := a.sums.Upsert(summary.Entry{ID: "2026-11-04-existing", Day: oldDay, Start: oldStart, End: oldStart.Add(time.Hour), Headline: "旧日期既有"}); err != nil {
		t.Fatal(err)
	}
	newStart := time.Date(2026, 11, 5, 10, 0, 0, 0, time.Local)
	newEntry := summary.Entry{ID: "2026-11-05-only", Day: newDay, Start: newStart, End: newStart.Add(time.Hour), Headline: "新日期摘要"}
	if err := a.sums.Upsert(newEntry); err != nil {
		t.Fatal(err)
	}

	jobs := make(chan func(), 2)
	a.post = func(f func()) { jobs <- f }
	a.dayFilter = oldDay
	a.blocks = []capture.Block{{Start: oldStart.Add(2 * time.Hour), End: oldStart.Add(150 * time.Minute), App: "Code", Samples: 2}}
	a.runSummary(false)

	<-reqStarted
	a.dayFilter = newDay
	a.sumEntries = []summary.Entry{newEntry}
	close(releaseReq)

	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("摘要任务超时")
	}

	if len(a.sumEntries) != 1 || a.sumEntries[0].Headline != "新日期摘要" {
		t.Fatalf("切天后旧摘要污染了新日期列表: %+v", a.sumEntries)
	}

	a.dayFilter = oldDay
	a.runSummary(false)
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("二次增量摘要超时")
	}
	if len(a.sumEntries) != 2 {
		t.Fatalf("增量摘要覆盖了既有摘要: %+v", a.sumEntries)
	}
}

func TestProviderTestIsolationAndAPIKeyRebuildsGenerator(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	p1, ok := providers.ByID("deepseek")
	if !ok {
		t.Fatal("missing deepseek preset")
	}
	p2, ok := providers.ByID("openai")
	if !ok {
		t.Fatal("missing openai preset")
	}
	a.addProvider(p1)
	a.addProvider(p1)
	a.addProvider(p2)
	if len(a.cfgNow.Providers) != 3 {
		t.Fatalf("providers = %d", len(a.cfgNow.Providers))
	}
	k0 := providerKey(a.cfgNow.Providers[0])
	k1 := providerKey(a.cfgNow.Providers[1])
	if k0 == k1 {
		t.Fatalf("同预设多实例键冲突: %q", k0)
	}

	a.tab, a.settingsTab = 1, 1
	a.openProvider = k0
	tt := ui.NewTester(a.view, 780, 1800)
	keyIn, ok := tt.Find("sk-")
	_ = keyIn
	_ = ok
	a.saveProviderField(0, func(p *config.ProviderConfig) {
		p.APIKey = "sk-updated"
		p.DefaultModel = "deepseek-chat"
	})
	a.setDefaultProvider(a.cfgNow.Providers[0])
	if a.gen == nil {
		t.Fatal("未创建生成器")
	}

	a.openProvider = k1
	tt = ui.NewTester(a.view, 780, 1800)
	reqStarted := make(chan struct{})
	releaseReq := make(chan struct{})
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		close(reqStarted)
		<-releaseReq
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"stale-model","choices":[{"message":{"content":"ok"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	jobs := make(chan func(), 2)
	a.post = func(f func()) { jobs <- f }
	a.testConnection(0)
	<-reqStarted
	a.removeProvider(0)
	close(releaseReq)
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("连接测试超时")
	}
	if a.providerTest.phase != testIdle || a.providerTest.model != "" {
		t.Fatalf("已删除供应商的过时测试结果回写到了全局状态: %+v", a.providerTest)
	}
}

func TestProviderKeyInputRebuildsGenerator(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	preset, ok := providers.ByID("openai")
	if !ok {
		t.Fatal("missing openai")
	}
	a.addProvider(preset)
	a.saveProviderField(0, func(p *config.ProviderConfig) {
		p.DefaultModel = "gpt-5.2"
	})
	a.setDefaultProvider(a.cfgNow.Providers[0])
	if a.gen == nil {
		t.Fatal("未创建生成器")
	}

	var seenAuth string
	a.llmc.HTTP.Transport = errorTestTransport(func(r *http.Request) (*http.Response, error) {
		seenAuth = r.Header.Get("Authorization")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"gpt-5.2","output":[{"type":"message","content":[{"type":"output_text","text":"{\"headline\":\"h\",\"summary\":\"s\"}"}]}]}`)),
			Header:     make(http.Header),
		}, nil
	})

	a.tab, a.settingsTab = 1, 1
	tt := ui.NewTester(a.view, 780, 1800)
	keyLabel, ok := tt.Find("接口密钥")
	if !ok {
		t.Fatal("missing key label")
	}
	tt.ClickAt(keyLabel.X+40, keyLabel.Y+28)
	if !tt.Focused("接口密钥") {
		t.Fatalf("API Key 输入框未聚焦: texts=%q", tt.Texts())
	}
	tt.Type("sk-live-secret")
	tt.Frame()

	start := time.Date(2026, 11, 4, 9, 0, 0, 0, time.Local)
	_, err := a.gen.GenerateDay(context.Background(), []capture.Block{{Start: start, End: start.Add(10 * time.Minute), App: "Code", Samples: 1}}, nil, "2026-11-04", time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if seenAuth != "Bearer sk-live-secret" {
		t.Fatalf("修改 API Key 后未重建生成器，发送授权头为 %q", seenAuth)
	}
}

func TestTabSwitchClearsDraftFocusPreventsZombieCommit(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab, a.settingsTab = 1, 0
	tt := ui.NewTester(a.view, 780, 2600)
	btn, ok := tt.Find("从运行中的应用添加")
	if !ok {
		t.Fatal("missing sensitive picker")
	}
	tt.ClickAt(btn.X, btn.Y+50)
	if !a.drafts.sensitiveFocus {
		t.Fatal("敏感应用输入框未处于聚焦态")
	}
	a.drafts.sensitiveDraft = "com.zombie.shouldnotcommit"
	a.settingsTab = 2
	tt.Frame()
	if a.drafts.sensitiveFocus {
		t.Fatal("切页后未清除草稿聚焦态")
	}
	a.updateRecorder(func(r *config.Recorder) { r.SensitiveApps = []string{"com.real.app"} })
	a.settingsTab = 0
	tt.Frame()
	if got := a.cfg.Get().Recorder.SensitiveApps; len(got) != 1 || got[0] != "com.real.app" {
		t.Fatalf("切回后僵尸草稿覆写了配置: %v", got)
	}
}

func TestPollStepCrossesDeadZoneAndResumeClearsPause(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.updateRecorder(func(r *config.Recorder) { r.ScreenPollMs = 10000 })
	if got := a.stepRecorderField(4, -1); got != 0 {
		t.Fatalf("10000 向下步进期望 0，得到 %d", got)
	}
	a.commitRecorderField(4, 0)
	if got := a.stepRecorderField(4, 1); got != 10000 {
		t.Fatalf("0 向上步进期望 10000，得到 %d", got)
	}

	future := time.Now().Add(time.Hour)
	a.setRecorderEnabled(true)
	a.pauseRecorder(future)
	if a.runner.Status().Phase != capture.PhasePaused {
		t.Fatalf("未进入暂停态: %v", a.runner.Status().Phase)
	}
	a.tab = 0
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, 900)
	if err := tt.Click("已暂停"); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Get().Recorder.Enabled || !a.cfg.Get().Recorder.PausedUntil.IsZero() || a.runner.Status().Phase == capture.PhasePaused || a.runner.Status().Phase == capture.PhaseOff {
		t.Fatalf("暂停状态下点开关未恢复录制: enabled=%v paused=%v phase=%v", a.cfg.Get().Recorder.Enabled, a.cfg.Get().Recorder.PausedUntil, a.runner.Status().Phase)
	}

	a.pauseRecorder(time.Now().Add(time.Hour))
	a.setRecorderEnabled(false)
	a.setRecorderEnabled(true)
	if !a.cfg.Get().Recorder.PausedUntil.IsZero() || a.runner.Status().Phase == capture.PhasePaused {
		t.Fatalf("停录再启未清除历史暂停时间: %v", a.runner.Status().Phase)
	}
}

func TestPermissionRequestPersistsAcrossRefreshAndReadWriteErrorIsolation(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.runner.SetPermissions(capture.Perms{Screen: capture.PermDenied, Accessibility: capture.PermDenied})
	a.status = a.runner.Status()
	a.requestScreen = func() capture.PermState { return capture.PermGranted }
	a.tab, a.settingsTab = 1, 0
	tt := ui.NewTester(a.view, 780, 2600)
	if err := tt.Click("请求授权"); err != nil {
		t.Fatal(err)
	}
	a.lastLoad = time.Time{}
	a.refresh()
	if a.status.Perms.Screen != capture.PermGranted {
		t.Fatalf("授权后 refresh() 覆写回退了权限状态: %v", a.status.Perms.Screen)
	}

	a.err = errors.New("save failed")
	a.lastLoad = time.Time{}
	a.refresh()
	if a.err == nil || a.bannerError() == nil {
		t.Fatal("空日期 loadDay 抹掉了配置保存错误")
	}
}

func TestPrintRevealZeroHeightRecoversAndLegacyRetry(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.paperFeed.day = a.dayKey()
	a.receiptRevision = 1
	a.printReveal.revision = 0

	var sim shredSimulation
	if sim.below(400) {
		t.Fatal("未初始化的碎纸模拟不应判定为已落完")
	}

	a.legacy.phase = legacyFailed
	a.tab, a.settingsTab = 1, 0
	tt := ui.NewTester(a.view, 780, 1200)
	_ = tt
	a.settingsTab = 2
	tt.Frame()
	if a.legacy.phase != legacyReady {
		t.Fatalf("切回通用页后失败的旧文件检查未重试恢复: %+v", a.legacy)
	}

	a.shutdown()
	if !a.tasks.isClosed() {
		t.Fatal("shutdown 未关闭任务作用域")
	}
}

func TestAutoSummaryLoopTargetsTodayAndShutdownStops(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	a.dayFilter = yesterday
	if a.todayKey() != today || a.dayKey() != yesterday {
		t.Fatalf("today=%s dayKey=%s", a.todayKey(), a.dayKey())
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		a.autoSummaryLoop(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("autoSummaryLoop 未响应上下文取消")
	}
}

func TestClearWaitsAfterModelSwitchCancelsActiveSummary(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	reqStarted := make(chan struct{})
	releaseReq := make(chan struct{})
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		close(reqStarted)
		<-releaseReq
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"fake","choices":[{"message":{"content":"{\"headline\":\"stale\",\"summary\":\"after-clear\"}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{
			{Preset: "openai", Label: "p1", ChatBase: "https://example.test/v1", DefaultModel: "m1"},
			{Preset: "openai", Label: "p2", ChatBase: "https://example.test/v1", DefaultModel: "m2"},
		}
	}); err != nil {
		t.Fatal(err)
	}
	a.rebuildGenerator()

	jobs := make(chan func(), 4)
	a.post = func(f func()) { jobs <- f }
	start := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	a.dayFilter = "2026-11-04"
	a.blocks = []capture.Block{{Start: start, End: start.Add(20 * time.Minute), App: "Code", Samples: 2}}
	a.runSummary(true)
	<-reqStarted

	a.setSummaryModel(providerKey(a.cfgNow.Providers[1]), "m2")
	a.clearActivity()

	time.Sleep(30 * time.Millisecond)
	close(releaseReq)
	deadline := time.After(10 * time.Second)
	for a.clear.running() {
		select {
		case f := <-jobs:
			f()
		case <-deadline:
			t.Fatal("清空未等待取消中的旧摘要任务完成")
		}
	}
	for len(jobs) > 0 {
		(<-jobs)()
	}
	if days := a.sums.Days(); len(days) != 0 {
		t.Fatalf("清空后过时摘要仍回写落盘: %v", days)
	}
}

func TestMultiInstanceOpenAIDefaultIsolationAndRemoveSecond(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	preset, ok := providers.ByID("openai")
	if !ok {
		t.Fatal("missing openai")
	}
	a.addProvider(preset)
	a.addProvider(preset)
	p0 := a.cfgNow.Providers[0]
	p1 := a.cfgNow.Providers[1]
	a.setDefaultProvider(p0)

	selected := resolveSummaryProviderKey(a.cfgNow.Summary.ProviderID, a.cfgNow.Providers)
	if selected != providerKey(p0) || selected == providerKey(p1) {
		t.Fatalf("同预设多实例默认解析错误: selected=%q p0=%q p1=%q", selected, providerKey(p0), providerKey(p1))
	}

	a.removeProvider(1)
	if len(a.cfgNow.Providers) != 1 || a.cfgNow.Summary.ProviderID != providerKey(p0) {
		t.Fatalf("删除第二个同名预设供应商清除了第一个默认项: %+v summary=%+v", a.cfgNow.Providers, a.cfgNow.Summary)
	}
}

func TestTimelineSummaryErrorScopedToFailedDayAndFetchButtonTriggers(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	a.summary.phase = summaryFailed
	a.summary.dayKey = "2026-11-04"
	a.summary.err = errors.New("boom-day-error")
	a.dayFilter = "2026-11-04"
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, 1800)
	if !tt.HasText("boom-day-error") {
		t.Fatalf("时间轴未显示失败日期的摘要错误: %q", tt.Texts())
	}
	a.dayFilter = "2026-11-05"
	tt.Frame()
	if tt.HasText("boom-day-error") {
		t.Fatalf("切换日期后仍显示其他天的摘要错误: %q", tt.Texts())
	}

	a.tab, a.settingsTab = 1, 1
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"gpt-5.2"},{"id":"gpt-5-mini"}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	preset, ok := providers.ByID("openai")
	if !ok {
		t.Fatal("missing openai")
	}
	a.addProvider(preset)
	jobs := make(chan func(), 2)
	a.post = func(f func()) { jobs <- f }
	tt.Frame()
	if err := tt.Click(locales.In("zh-Hans").ModelsFetch()); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("拉取模型按钮触发后超时")
	}
	tt.Frame()
	if !tt.HasText(locales.In("zh-Hans").ModelsFetchSuccess(2)) {
		t.Fatalf("未渲染拉取模型结果: %q", tt.Texts())
	}
}

func TestSetDefaultProviderCancelsInFlightSummary(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	reqStarted := make(chan struct{})
	releaseReq := make(chan struct{})
	a.llmc.HTTP.Transport = errorTestTransport(func(*http.Request) (*http.Response, error) {
		close(reqStarted)
		<-releaseReq
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"model":"m1","choices":[{"message":{"content":"{\"headline\":\"stale-a\",\"summary\":\"from-a\"}"}}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{
			{Preset: "openai", Label: "A", ChatBase: "https://a.example/v1", DefaultModel: "m1"},
			{Preset: "openai", Label: "B", ChatBase: "https://b.example/v1", DefaultModel: "m2"},
		}
	}); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.setDefaultProvider(a.cfgNow.Providers[0])

	jobs := make(chan func(), 4)
	a.post = func(f func()) { jobs <- f }
	start := time.Date(2026, 11, 4, 10, 0, 0, 0, time.Local)
	a.dayFilter = "2026-11-04"
	a.blocks = []capture.Block{{Start: start, End: start.Add(20 * time.Minute), App: "Code", Samples: 2}}
	a.runSummary(true)
	<-reqStarted

	a.setDefaultProvider(a.cfgNow.Providers[1])
	close(releaseReq)
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("取消后的摘要任务未返回")
	}
	if days := a.sums.Days(); len(days) != 0 {
		t.Fatalf("切换默认供应商后旧模型摘要仍写入落盘: %v", days)
	}
}

func TestMissingModelProviderConfigErrorVisibleOnTimeline(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "zhipu", APIKey: "sk-zhipu"}}
	}); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.setDefaultProvider(a.cfgNow.Providers[0])
	if a.gen != nil || a.summary.phase != summaryFailed || a.summary.err == nil {
		t.Fatalf("缺模型供应商状态异常: gen=%v summary=%+v", a.gen, a.summary)
	}
	a.tab = 0
	a.dayFilter = "2026-11-04"
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, 1000)
	want := locales.In("zh-Hans").ErrorNoModel()
	if !tt.HasText(want) {
		t.Fatalf("时间轴未显示缺模型错误 %q: %q", want, tt.Texts())
	}

	a.saveProviderField(0, func(p *config.ProviderConfig) { p.DefaultModel = "glm-4.6" })
	a.rebuildGenerator()
	tt.Frame()
	if tt.HasText(want) {
		t.Fatalf("配置恢复后仍残余缺模型错误 %q: %q", want, tt.Texts())
	}
}

func TestAnthropicPresetFetchModelsButtonHitsV1Models(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	preset, ok := providers.ByID("anthropic")
	if !ok {
		t.Fatal("missing anthropic preset")
	}
	a.addProvider(preset)
	a.saveProviderField(0, func(p *config.ProviderConfig) { p.APIKey = "sk-ant-btn" })

	var gotURL, gotVersion, gotKey string
	a.llmc.HTTP.Transport = errorTestTransport(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		gotVersion = r.Header.Get("anthropic-version")
		gotKey = r.Header.Get("x-api-key")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"claude-sonnet-4-5"}]}`)),
			Header:     make(http.Header),
		}, nil
	})
	a.tab, a.settingsTab = 1, 1
	jobs := make(chan func(), 2)
	a.post = func(f func()) { jobs <- f }
	tt := ui.NewTester(a.view, 780, 1800)
	if err := tt.Click(locales.In("zh-Hans").ModelsFetch()); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("Anthropic 拉取模型超时")
	}
	tt.Frame()
	if gotURL != "https://api.anthropic.com/v1/models" || gotVersion != "2023-06-01" || gotKey != "sk-ant-btn" {
		t.Fatalf("Anthropic 拉取请求参数错误: url=%q version=%q key=%q", gotURL, gotVersion, gotKey)
	}
	if !tt.HasText(locales.In("zh-Hans").ModelsFetchSuccess(1)) {
		t.Fatalf("未渲染 Anthropic 拉取结果: %q", tt.Texts())
	}
}

func TestClearActivityPreservesUnresolvedProviderConfigError(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "zhipu", APIKey: "sk-zhipu"}}
	}); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.setDefaultProvider(a.cfgNow.Providers[0])
	if a.gen != nil || a.summary.phase != summaryFailed || !errors.Is(a.summary.err, llm.ErrNoModel) {
		t.Fatalf("初始缺模型状态异常: gen=%v summary=%+v", a.gen, a.summary)
	}

	jobs := make(chan func(), 4)
	a.post = func(f func()) { jobs <- f }
	a.tab = 1
	a.clearActivity()
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("清空活动超时")
	}
	if a.clear.phase != clearIdle || a.gen != nil || a.summary.phase != summaryFailed || a.summary.dayKey != "" || !errors.Is(a.summary.err, llm.ErrNoModel) {
		t.Fatalf("清空后丢失了未解决的供应商配置错误: clear=%+v gen=%v summary=%+v", a.clear, a.gen, a.summary)
	}
	a.lastLoad = time.Now().Add(time.Hour)
	tt := ui.NewTester(a.view, 780, 1000)
	want := locales.In("zh-Hans").ErrorNoModel()
	if !tt.HasText(want) {
		t.Fatalf("清空活动后时间轴不再显示缺模型错误 %q: %q", want, tt.Texts())
	}

	a.saveProviderField(0, func(p *config.ProviderConfig) { p.DefaultModel = "glm-4.6" })
	a.summary.phase = summaryFailed
	a.summary.dayKey = "2026-11-04"
	a.summary.err = errors.New("stale-activity-err")
	a.tab = 1
	a.clearActivity()
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("二次清空活动超时")
	}
	if a.summary.phase != summaryIdle || a.summary.err != nil {
		t.Fatalf("供应商已恢复后清空活动未清除旧任务错误: %+v", a.summary)
	}
}

func TestStaleWindowDeletePreservesRecreatedProviderAndAddExpandsRow(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()

	if err := a.cfg.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "openai", Label: "shared", ChatBase: "https://first.example/v1"}}
	}); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()

	b, err := config.Open(a.appDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(c *config.Config) { c.Providers = nil }); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(c *config.Config) {
		c.Providers = []config.ProviderConfig{{Preset: "openai", Label: "shared", ChatBase: "https://second.example/v1"}}
	}); err != nil {
		t.Fatal(err)
	}

	a.removeProvider(0)
	reopened, err := config.Open(a.appDir)
	if err != nil {
		t.Fatal(err)
	}
	ps := reopened.Get().Providers
	if len(ps) != 1 || ps[0].ChatBase != "https://second.example/v1" {
		t.Fatalf("旧窗口误删了同标签新建供应商: %+v", ps)
	}

	a.cfgNow = reopened.Get()
	if err := b.Update(func(c *config.Config) { c.Providers = nil }); err != nil {
		t.Fatal(err)
	}
	preset, ok := providers.ByID("openai")
	if !ok {
		t.Fatal("missing openai preset")
	}
	a.addProvider(preset)
	if len(a.cfgNow.Providers) != 1 {
		t.Fatalf("跨句柄清空后添加供应商失败: %+v", a.cfgNow.Providers)
	}
	wantKey := providerKey(a.cfgNow.Providers[0])
	if a.openProvider != wantKey || a.openProvider == "openai\x00" {
		t.Fatalf("新增供应商行未绑定新 ID 键: open=%q want=%q", a.openProvider, wantKey)
	}
	a.tab, a.settingsTab = 1, 1
	tt := ui.NewTester(a.view, 780, 1800)
	if !tt.HasText(locales.In("zh-Hans").ModelsFetch()) {
		t.Fatalf("新增供应商面板未展开: %q", tt.Texts())
	}
}
