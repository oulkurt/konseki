/*
 * [INPUT]: 依赖临时应用配置、供应商预设、语言目录、离屏渲染器和假模型端点。
 * [OUTPUT]: 验证拉取按钮、订阅面板、密钥提示与链接、可用模型勾选、默认模型圆圈、添加弹窗和导入标记。
 * [POS]: 供应商配置界面的回归测试。网络请求全部指向 httptest，不联网。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"konseki/internal/config"
	"konseki/internal/providers"
	"konseki/locales"

	"github.com/egoist/mygo/ui"
)

// configureProvider 写入一个供应商并展开它的配置面板。
func configureProvider(t *testing.T, a *app, pc config.ProviderConfig) {
	t.Helper()
	if err := a.cfg.Update(func(c *config.Config) { c.Providers = []config.ProviderConfig{pc} }); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	a.tab, a.settingsTab = 1, 1
	a.openProvider = providerKey(pc)
}

func TestProviderFetchModelsButtonWritesList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("请求路径: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "alpha"}, {"id": "text-embedding-3"}, {"id": "beta"},
		}})
	}))
	defer server.Close()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.llmc.HTTP = server.Client()
	msg := locales.In("zh-Hans")
	configureProvider(t, a, config.ProviderConfig{Preset: "custom", ChatBase: server.URL + "/v1", DefaultModel: "alpha"})
	jobs := make(chan func(), 1)
	a.post = func(f func()) { jobs <- f }
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if err := tt.Click(msg.ModelsFetch()); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("拉取模型超时")
	}
	tt.Frame()
	// 嵌入模型不进聊天模型列表。发现清单保留全量，勾选默认全选。
	pc := a.cfg.Get().Providers[0]
	if !slices.Equal(pc.EnabledModels, []string{"alpha", "beta"}) {
		t.Fatalf("EnabledModels = %v", pc.EnabledModels)
	}
	if !slices.Equal(pc.DiscoveredModels, []string{"alpha", "beta"}) {
		t.Fatalf("DiscoveredModels = %v", pc.DiscoveredModels)
	}
	if !tt.HasText(msg.ModelsFetchSuccess(2)) {
		t.Fatalf("没有显示拉取结果：%q", tt.Texts())
	}
	// 拉回来的列表直接成为可选模型。
	if !tt.HasText(msg.ModelsAvailable()) || !tt.HasText("beta") {
		t.Fatalf("没有显示可用模型：%q", tt.Texts())
	}
}

func TestOAuthProviderPanelHidesKeyAndShowsExpiry(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	expiry := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	pc := config.ProviderConfig{
		Preset: "openai", Label: "Codex", Auth: "oauth", AccessToken: "token",
		DefaultModel: "gpt-5.2", ExpiresAt: expiry,
		EnabledModels: []string{"gpt-5.2", "gpt-5.2-codex"},
	}
	configureProvider(t, a, pc)
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	// OAuth 没有 key 可填，端点由 Resolve 写死，都不该出现输入框。
	for _, forbidden := range []string{msg.ModelsKey(), msg.ModelsURL(), msg.ModelsResponsesURL()} {
		if _, ok := tt.Find(forbidden); ok {
			t.Errorf("订阅面板显示了 %q", forbidden)
		}
	}
	if !tt.HasText(msg.ModelsAuthSubscription()) || !tt.HasText(msg.ModelsAuthExpires(expiry.Format(expiryDateLayout))) {
		t.Fatalf("订阅凭据行不完整：%q", tt.Texts())
	}
	if !tt.HasText(msg.ModelsEndpointSubscription()) || !tt.HasText(msg.ModelsTest()) {
		t.Fatal("订阅面板缺少端点说明或测试按钮")
	}
	// ChatGPT 订阅端点没有通用的 /models，不显示拉取按钮。
	if _, ok := tt.Find(msg.ModelsFetch()); ok {
		t.Error("OpenAI 订阅显示了拉取按钮")
	}
	// 到期时间已过时显示「已过期」。
	if err := a.cfg.Update(func(c *config.Config) { c.Providers[0].ExpiresAt = time.Now().Add(-24 * time.Hour) }); err != nil {
		t.Fatal(err)
	}
	a.cfgNow = a.cfg.Get()
	tt.Frame()
	if !tt.HasText(msg.ModelsAuthExpired()) {
		t.Fatalf("过期凭据没有显示已过期：%q", tt.Texts())
	}
}

func TestGeminiOAuthProviderOffersFetch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	configureProvider(t, a, config.ProviderConfig{
		Preset: "gemini-cli", Label: "Gemini CLI", Auth: "oauth",
		AccessToken: "token", DefaultModel: "gemini-2.5-pro",
	})
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find(msg.ModelsFetch()); !ok {
		t.Fatalf("Gemini 订阅缺少拉取按钮：%q", tt.Texts())
	}
}

// 没有 /models 可拉的家直接把预设和档位的模型列出来。
// 用户不必手打模型 id，摘要菜单也有完整的候选。
func TestNoListProviderShowsPresetModels(t *testing.T) {
	preset, ok := providers.ByID("baidu-qianfan")
	if !ok || len(preset.Models) == 0 || !preset.NoList {
		t.Fatal("预设夹具失效：baidu-qianfan 需要 Models 和 NoList")
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab, a.settingsTab = 1, 1
	a.addProvider(preset)
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find(msg.ModelsFetch()); ok {
		t.Fatal("NoList 供应商显示了拉取按钮")
	}
	if !tt.HasText(msg.ModelsAvailable()) {
		t.Fatalf("缺少预设模型列表：%q", tt.Texts())
	}
	// 点行尾圆圈把模型设为默认；列表行本身只负责勾选。
	if err := tt.Click(msg.ModelsDefault() + " deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}
	if got := a.cfg.Get().Providers[0].DefaultModel; got != "deepseek-v4-pro" {
		t.Fatalf("点设为默认后默认模型是 %q", got)
	}
}

func TestProviderKeyHintAndKeyLink(t *testing.T) {
	preset, ok := providers.ByID("opencode-zen")
	if !ok {
		t.Fatal("缺少 opencode-zen 预设")
	}
	if preset.KeyHint == "" || preset.KeysURL == "" {
		t.Fatal("预设缺少 KeyHint 或 KeysURL，这个测试失去意义")
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	opened := ""
	a.openExternal = func(url string) error { opened = url; return nil }
	a.tab, a.settingsTab = 1, 1
	a.addProvider(preset)
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(preset.KeyHint) {
		t.Fatalf("缺少密钥提示：%q", tt.Texts())
	}
	if err := tt.Click(msg.ModelsGetKey()); err != nil {
		t.Fatal(err)
	}
	if opened != preset.KeysURL {
		t.Fatalf("打开的地址是 %q，期望 %q", opened, preset.KeysURL)
	}
}

// 勾选列表：点行翻勾选，写入 EnabledModels；第一次勾选把当前列表固化成
// 发现清单，取消的模型仍留在列表里，用户能再勾回来。
func TestProviderModelCheckboxTogglesEnabled(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	configureProvider(t, a, config.ProviderConfig{
		Preset: "custom", ChatBase: "https://example.test/v1",
		DefaultModel: "alpha", EnabledModels: []string{"alpha", "beta"},
	})
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(msg.ModelsAvailableHint()) {
		t.Fatalf("缺少可用模型说明：%q", tt.Texts())
	}
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	pc := a.cfg.Get().Providers[0]
	if !slices.Equal(pc.EnabledModels, []string{"alpha"}) {
		t.Fatalf("取消勾选后的 EnabledModels = %v", pc.EnabledModels)
	}
	if !slices.Equal(pc.DiscoveredModels, []string{"alpha", "beta"}) {
		t.Fatalf("第一次勾选应固化发现清单，得到 %v", pc.DiscoveredModels)
	}
	if pc.DefaultModel != "alpha" {
		t.Fatalf("取消非默认模型不该改默认，得到 %q", pc.DefaultModel)
	}
	if got := providerModelChoices(pc); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("取消勾选后候选是 %v，期望只剩 alpha", got)
	}
	// 取消的模型仍在列表里，再点一次勾回来，行不跳位。
	if _, ok := tt.Find("beta"); !ok {
		t.Fatalf("取消勾选的模型从列表里消失了：%q", tt.Texts())
	}
	if err := tt.Click("beta"); err != nil {
		t.Fatal(err)
	}
	pc = a.cfg.Get().Providers[0]
	if !slices.Equal(pc.EnabledModels, []string{"alpha", "beta"}) {
		t.Fatalf("重新勾选后的 EnabledModels = %v", pc.EnabledModels)
	}
}

// 行尾圆圈把勾选的模型设为默认，默认行显示徽标。
func TestProviderModelDefaultBadgeAndCircle(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	configureProvider(t, a, config.ProviderConfig{
		Preset: "custom", ChatBase: "https://example.test/v1",
		DefaultModel: "alpha", EnabledModels: []string{"alpha", "beta"},
	})
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if _, ok := tt.Find(msg.ModelsDefaultBadge()); !ok {
		t.Fatalf("默认模型没有徽标：%q", tt.Texts())
	}
	if err := tt.Click(msg.ModelsDefault() + " beta"); err != nil {
		t.Fatal(err)
	}
	pc := a.cfg.Get().Providers[0]
	if pc.DefaultModel != "beta" {
		t.Fatalf("点圆圈后默认模型是 %q", pc.DefaultModel)
	}
	tt.Frame()
	// 徽标跟着默认模型走，圆圈从 beta 换到 alpha。
	if _, ok := tt.Find(msg.ModelsDefaultBadge()); !ok {
		t.Fatalf("换默认后没有徽标：%q", tt.Texts())
	}
	if _, ok := tt.Find(msg.ModelsDefault() + " beta"); ok {
		t.Fatalf("beta 已是默认，不该还有设为默认的圆圈：%q", tt.Texts())
	}
	if _, ok := tt.Find(msg.ModelsDefault() + " alpha"); !ok {
		t.Fatalf("alpha 不再是默认，缺少设为默认的圆圈：%q", tt.Texts())
	}
}

// 全选/全不选是一个按钮，按当前状态切换文案。
// 全不选后摘要菜单的候选为空；列表行还在，用户能重新全选。
func TestProviderModelsSelectAllToggle(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	configureProvider(t, a, config.ProviderConfig{
		Preset: "custom", ChatBase: "https://example.test/v1",
		DefaultModel: "alpha", EnabledModels: []string{"alpha", "beta"},
	})
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(msg.ModelsDeselectAll()) {
		t.Fatalf("全选状态下缺少全不选按钮：%q", tt.Texts())
	}
	if err := tt.Click(msg.ModelsDeselectAll()); err != nil {
		t.Fatal(err)
	}
	pc := a.cfg.Get().Providers[0]
	if len(pc.EnabledModels) != 0 {
		t.Fatalf("全不选后 EnabledModels = %v", pc.EnabledModels)
	}
	if !slices.Equal(pc.DiscoveredModels, []string{"alpha", "beta"}) {
		t.Fatalf("全不选后发现清单 = %v", pc.DiscoveredModels)
	}
	if pc.DefaultModel != "" {
		t.Fatalf("全不选后默认模型 = %q，期望清空", pc.DefaultModel)
	}
	if got := providerModelChoices(pc); len(got) != 0 {
		t.Fatalf("全不选后摘要菜单候选 = %v，期望为空", got)
	}
	tt.Frame()
	if !tt.HasText(msg.ModelsSelectAll()) {
		t.Fatalf("全不选后缺少全选按钮：%q", tt.Texts())
	}
	if _, ok := tt.Find("beta"); !ok {
		t.Fatalf("全不选后列表行不该消失：%q", tt.Texts())
	}
	if err := tt.Click(msg.ModelsSelectAll()); err != nil {
		t.Fatal(err)
	}
	pc = a.cfg.Get().Providers[0]
	if !slices.Equal(pc.EnabledModels, []string{"alpha", "beta"}) {
		t.Fatalf("全选后 EnabledModels = %v", pc.EnabledModels)
	}
	if pc.DefaultModel != "alpha" {
		t.Fatalf("全选后默认模型 = %q，期望回到第一个", pc.DefaultModel)
	}
	if got := providerModelChoices(pc); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Fatalf("全选后摘要菜单候选 = %v", got)
	}
}

func TestEnabledModelsFoldBeyondPreview(t *testing.T) {
	ids := make([]string, 0, providerModelPreview+2)
	for i := range providerModelPreview + 2 {
		ids = append(ids, fmt.Sprintf("model-%02d", i))
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	configureProvider(t, a, config.ProviderConfig{
		Preset: "custom", ChatBase: "https://example.test/v1",
		DefaultModel: ids[0], EnabledModels: ids,
	})
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(msg.ModelsMore(2)) {
		t.Fatalf("缺少折叠入口：%q", tt.Texts())
	}
	if _, ok := tt.Find(ids[len(ids)-1]); ok {
		t.Fatal("折叠的模型不该显示")
	}
	if err := tt.Click(msg.ModelsMore(2)); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find(ids[len(ids)-1]); !ok {
		t.Fatalf("展开后仍缺少 %q：%q", ids[len(ids)-1], tt.Texts())
	}
}

func TestAddProviderModalShowsKindAndNote(t *testing.T) {
	preset, ok := providers.ByID("azure")
	if !ok {
		t.Fatal("缺少 azure 预设")
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab, a.settingsTab = 1, 1
	a.addProviderOpen = true
	a.providerSearch = "azure"
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(preset.Note) {
		t.Fatalf("添加弹窗缺少预设说明：%q", tt.Texts())
	}
	if !tt.HasText(msg.ModelsKindVendor()) {
		t.Fatalf("添加弹窗缺少种类标记：%q", tt.Texts())
	}
}

func TestImportRowsShowCredentialKind(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.tab, a.settingsTab = 1, 1
	a.detectStarted, a.detectDone = true, true
	a.detected = []providers.Credential{
		{Source: "one", Label: "local credential", Kind: "oauth"},
		{Source: "two", Label: "second credential", Kind: "api_key"},
	}
	msg := locales.In("zh-Hans")
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if !tt.HasText(msg.ModelsAuthSubscription()) {
		t.Fatalf("订阅凭据缺少标记：%q", tt.Texts())
	}
	if !tt.HasText(msg.ModelsCredentialAPIKey()) {
		t.Fatalf("API key 凭据缺少标记：%q", tt.Texts())
	}
}

// 添加弹窗按种类分组：官方在前、中转居中、本地在后。
// 目录末尾追加的 gemini-cli 是官方预设，排序后要回到官方组里。
func TestProviderPickerKindsGrouped(t *testing.T) {
	last, lastID := -1, ""
	for _, preset := range sortedPresets() {
		r := providerKindRank(preset.Kind)
		if r < last {
			t.Fatalf("%s 的 %s 排在 %s 之后", preset.ID, preset.Kind, lastID)
		}
		last, lastID = r, preset.ID
	}
	seenRelay := false
	for _, preset := range sortedPresets() {
		switch preset.Kind {
		case providers.KindRelay:
			seenRelay = true
		case providers.KindVendor:
			if seenRelay {
				t.Fatalf("官方预设 %s 排在中转之后", preset.ID)
			}
		}
	}
}

func TestProviderPanelStringsBothLanguages(t *testing.T) {
	for _, lang := range []string{"zh-Hans", "en"} {
		msg := locales.In(lang)
		values := map[string]string{
			"modelsSectionAuth":          msg.ModelsSectionAuth(),
			"modelsSectionEndpoint":      msg.ModelsSectionEndpoint(),
			"modelsSectionModel":         msg.ModelsSectionModel(),
			"modelsSectionActions":       msg.ModelsSectionActions(),
			"modelsAuthSubscription":     msg.ModelsAuthSubscription(),
			"modelsCredentialAPIKey":     msg.ModelsCredentialAPIKey(),
			"modelsAuthExpires":          msg.ModelsAuthExpires("2026-12-01"),
			"modelsAuthExpired":          msg.ModelsAuthExpired(),
			"modelsEndpointSubscription": msg.ModelsEndpointSubscription(),
			"modelsGetKey":               msg.ModelsGetKey(),
			"modelsFetch":                msg.ModelsFetch(),
			"modelsFetching":             msg.ModelsFetching(),
			"modelsAvailable":            msg.ModelsAvailable(),
			"modelsAvailableHint":        msg.ModelsAvailableHint(),
			"modelsSelectAll":            msg.ModelsSelectAll(),
			"modelsDeselectAll":          msg.ModelsDeselectAll(),
			"modelsDefaultBadge":         msg.ModelsDefaultBadge(),
			"modelsMore":                 msg.ModelsMore(2),
			"modelsResponsesURL":         msg.ModelsResponsesURL(),
			"modelsKindVendor":           msg.ModelsKindVendor(),
			"modelsKindRelay":            msg.ModelsKindRelay(),
			"modelsKindLocal":            msg.ModelsKindLocal(),
			"modelsKindRemote":           msg.ModelsKindRemote(),
		}
		for key, value := range values {
			if strings.TrimSpace(value) == "" || value == key {
				t.Errorf("%s 缺少 %s", lang, key)
				continue
			}
			if lang == "en" && strings.ContainsFunc(value, isCJK) {
				t.Errorf("%s 的 %s 混入中文：%q", lang, key, value)
			}
		}
	}
}

// 拉模型列表不需要先有模型：新配的按量供应商往往还没有默认模型，
// 拉取恰恰是选模型的第一步。Resolve 报「尚未选择模型」时 Kind 和端点都已备好，
// 照常去拉（真机 DeepSeek 实测暴露的死锁）。
func TestProviderFetchWithoutModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "deepseek-chat"}, {"id": "deepseek-reasoner"},
		}})
	}))
	defer server.Close()
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.llmc.HTTP = server.Client()
	msg := locales.In("zh-Hans")
	// 复刻 DeepSeek 实测形态：有端点、无预设模型、未选默认模型。
	configureProvider(t, a, config.ProviderConfig{Preset: "custom", ChatBase: server.URL + "/v1"})
	jobs := make(chan func(), 1)
	a.post = func(f func()) { jobs <- f }
	tt := ui.NewTester(a.view, 780, 3000)
	tt.SetLocale("zh-Hans")
	if err := tt.Click(msg.ModelsFetch()); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-jobs:
		f()
	case <-time.After(10 * time.Second):
		t.Fatal("拉取模型超时")
	}
	tt.Frame()
	pc := a.cfg.Get().Providers[0]
	if !slices.Equal(pc.EnabledModels, []string{"deepseek-chat", "deepseek-reasoner"}) {
		t.Fatalf("EnabledModels = %v", pc.EnabledModels)
	}
	if !slices.Equal(pc.DiscoveredModels, []string{"deepseek-chat", "deepseek-reasoner"}) {
		t.Fatalf("DiscoveredModels = %v", pc.DiscoveredModels)
	}
	if pc.DefaultModel != "deepseek-chat" {
		t.Fatalf("拉取后默认模型 = %q", pc.DefaultModel)
	}
	if !tt.HasText(msg.ModelsFetchSuccess(2)) {
		t.Fatalf("没有显示拉取结果：%q", tt.Texts())
	}
}
