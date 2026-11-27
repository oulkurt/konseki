/*
 * [INPUT]: 依赖临时设置文件与配置存储。
 * [OUTPUT]: 验证迁移、默认值、键鼠计数配置往返和归一化边界。
 * [POS]: 配置模块的磁盘契约测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func openFixture(t *testing.T, data string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestMigration(t *testing.T) {
	for _, tc := range []struct {
		extra string
		days  int
	}{{"", 14}, {`,"retentionDays":7`, 7}} {
		t.Run(tc.extra, func(t *testing.T) {
			original := `{"providers":[{"preset":"p","label":"l"}],"redact":{"enabled":false,"secrets":false,"personal":false,"showHits":false},"summary":{"language":"en"},"capturePaused":true` + tc.extra + `}`
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.json"), []byte(original), 0600)
			os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(`{"p|l":{"apiKey":"secret"}}`), 0600)
			s, err := Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			c := s.Get()
			if c.Language != "en" || c.Recorder.Enabled || c.Recorder.RetentionDays != tc.days || c.Redact.Enabled || c.Providers[0].APIKey != "secret" {
				t.Fatalf("迁移结果: %+v", c)
			}
			backup := filepath.Join(dir, "config.v1.json")
			b, err := os.ReadFile(backup)
			if err != nil || string(b) != original {
				t.Fatalf("备份: %q %v", b, err)
			}
			if err := os.WriteFile(backup, []byte("保留备份"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = Open(dir); err != nil {
				t.Fatal(err)
			}
			b, _ = os.ReadFile(backup)
			if string(b) != "保留备份" {
				t.Fatal("备份被覆盖")
			}
		})
	}
}

func TestPresenceAndRoundTrip(t *testing.T) {
	s, _ := openFixture(t, `{"version":2,"recorder":{"enabled":false,"screenPollMs":0,"retentionDays":0},"redact":{"enabled":false,"secrets":false,"personal":false,"showHits":false}}`)
	c := s.Get()
	if c.Recorder.Enabled || c.Recorder.ScreenPollMs != 0 || c.Recorder.RetentionDays != 0 || !reflect.DeepEqual(c.Redact, RedactConfig{}) {
		t.Fatalf("显式零值: %+v", c)
	}
	if err := s.Update(func(c *Config) {}); err != nil {
		t.Fatal(err)
	}
	s, err := Open(s.dir)
	if err != nil || s.Get().Recorder.RetentionDays != 0 {
		t.Fatalf("永久保留: %v", err)
	}
	s, _ = openFixture(t, `{"version":2}`)
	if !s.Get().Recorder.Enabled {
		t.Fatal("默认未启用")
	}
	d := Default()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = openFixture(t, string(data))
	if !reflect.DeepEqual(s.Get(), d) {
		t.Fatalf("默认值往返不一致: %#v", s.Get())
	}
}

func TestNormalization(t *testing.T) {
	cases := []struct {
		key    string
		lo, hi int
	}{{"debounceMs", 3000, 30000}, {"heartbeatMs", 60000, 300000}, {"idleMs", 10000, 600000}, {"typingPauseMs", 800, 5000}, {"screenPollMs", 10000, 30000}, {"jpegQuality", 30, 95}, {"maxStorageMB", 100, 200000}, {"retentionDays", 1, 3650}, {"ocr.everyN", 1, 20}}
	for _, tc := range cases {
		for _, v := range []int{-1, 999999} {
			t.Run(tc.key, func(t *testing.T) {
				r := map[string]any{}
				if tc.key == "ocr.everyN" {
					r["ocr"] = map[string]int{"everyN": v}
				} else {
					r[tc.key] = v
				}
				b, _ := json.Marshal(map[string]any{"version": 2, "recorder": r})
				s, _ := openFixture(t, string(b))
				r2, _ := json.Marshal(s.Get().Recorder)
				var got map[string]any
				json.Unmarshal(r2, &got)
				var n float64
				if tc.key == "ocr.everyN" {
					n = got["ocr"].(map[string]any)["everyN"].(float64)
				} else {
					n = got[tc.key].(float64)
				}
				want := tc.lo
				if v > 0 {
					want = tc.hi
				}
				if int(n) != want {
					t.Fatalf("%s=%v，期望 %d", tc.key, n, want)
				}
			})
		}
	}
	for _, tc := range []struct{ in, want string }{{"en-US", "en"}, {"zh", "zh-Hans"}, {"", "zh-Hans"}} {
		b, _ := json.Marshal(map[string]any{"version": 2, "language": tc.in})
		s, _ := openFixture(t, string(b))
		if s.Get().Language != tc.want {
			t.Fatal(tc)
		}
	}
	s, _ := openFixture(t, `{"version":2,"recorder":{"sensitiveApps":[" A ","a"," ","B"],"ocr":{"languages":[" en-US ","en-US"," "]}}}`)
	if !reflect.DeepEqual(s.Get().Recorder.SensitiveApps, []string{"A", "B"}) || !reflect.DeepEqual(s.Get().Recorder.OCR.Languages, []string{"en-US"}) {
		t.Fatal(s.Get().Recorder)
	}
	t.Setenv("KONSEKI_HOME", "/tmp/konseki-test")
	if AppDir() != "/tmp/konseki-test" {
		t.Fatal(AppDir())
	}
}

func TestClearedSensitiveAppsSurviveReload(t *testing.T) {
	s, dir := openFixture(t, `{"version":2}`)
	if err := s.Update(func(c *Config) { c.Recorder.SensitiveApps = nil }); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Recorder.SensitiveApps; len(got) != 0 {
		t.Fatalf("清空的敏感应用重启后又回来了: %q", got)
	}
}

func TestInputCountsMigrationAndRoundTrip(t *testing.T) {
	s, dir := openFixture(t, `{"version":2,"recorder":{"inputMonitor":true}}`)
	if s.Get().Recorder.InputCounts {
		t.Fatal("旧键不能开启键鼠计数")
	}
	if err := s.Update(func(c *Config) { c.Recorder.InputCounts = true }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || strings.Contains(string(data), "inputMonitor") {
		t.Fatalf("旧键未移除: %s %v", data, err)
	}
	s, err = Open(dir)
	if err != nil || !s.Get().Recorder.InputCounts {
		t.Fatalf("新键未往返: %v", err)
	}
}

func TestUpdateFailurePreservesInMemoryConfig(t *testing.T) {
	s, dir := openFixture(t, `{"version":2,"language":"en"}`)
	if err := os.Mkdir(filepath.Join(dir, "config.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := s.Update(func(c *Config) {
		c.Language = "zh-Hans"
		c.Providers = append(c.Providers, ProviderConfig{Preset: "openai", APIKey: "sk-test"})
	})
	if err == nil {
		t.Fatal("expected Update error when config.json.tmp is a directory")
	}
	got := s.Get()
	if got.Language != "en" || len(got.Providers) != 0 {
		t.Fatalf("in-memory config mutated on failed Update: %+v", got)
	}
}

func TestDuplicatePresetLabelSecretsIsolation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(c *Config) {
		c.Providers = []ProviderConfig{
			{Preset: "openai", Label: "work", Auth: "api_key", APIKey: "key-1"},
			{Preset: "openai", Label: "work", Auth: "api_key", APIKey: "key-2"},
			{Preset: "a|b", Label: "c", APIKey: "pipe-1"},
			{Preset: "a", Label: "b|c", APIKey: "pipe-2"},
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	got := s.Get().Providers
	if len(got) != 4 {
		t.Fatalf("expected 4 providers, got %d", len(got))
	}
	ids := map[string]bool{}
	for i, p := range got {
		if p.ID == "" {
			t.Fatalf("provider %d has empty ID", i)
		}
		if ids[p.ID] {
			t.Fatalf("duplicate ID %q", p.ID)
		}
		ids[p.ID] = true
		if i < 2 && p.Auth != "apikey" {
			t.Fatalf("provider %d auth = %q, want apikey", i, p.Auth)
		}
	}

	reloaded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rp := reloaded.Get().Providers
	wantKeys := []string{"key-1", "key-2", "pipe-1", "pipe-2"}
	for i, want := range wantKeys {
		if rp[i].APIKey != want {
			t.Fatalf("provider %d APIKey = %q, want %q", i, rp[i].APIKey, want)
		}
		if rp[i].ID != got[i].ID {
			t.Fatalf("provider %d ID changed on reload: %q vs %q", i, rp[i].ID, got[i].ID)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "expiresAt") {
		t.Fatalf("zero ExpiresAt should be omitted: %s", raw)
	}
	if err := s.Update(func(c *Config) {
		c.Providers[0].ExpiresAt = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	}); err != nil {
		t.Fatal(err)
	}
	reloaded2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded2.Get().Providers[0].ExpiresAt.IsZero() {
		t.Fatal("non-zero ExpiresAt lost on reload")
	}
}

func TestExplicitV1AndV2MissingVersionMigration(t *testing.T) {
	v1 := `{"version":1,"providers":[{"preset":"p","label":"l"}],"summary":{"language":"en"},"capturePaused":true,"retentionDays":9}`
	s1, dir1 := openFixture(t, v1)
	c1 := s1.Get()
	if c1.Version != 2 || c1.Language != "en" || c1.Recorder.Enabled || c1.Recorder.RetentionDays != 9 {
		t.Fatalf("explicit v1 migration failed: %+v", c1)
	}
	if _, err := os.Stat(filepath.Join(dir1, "config.v1.json")); err != nil {
		t.Fatalf("missing config.v1.json backup for explicit v1: %v", err)
	}

	v2NoVer := `{"language":"en","recorder":{"enabled":false,"retentionDays":45,"debounceMs":5000}}`
	s2, dir2 := openFixture(t, v2NoVer)
	c2 := s2.Get()
	if c2.Version != 2 || c2.Language != "en" || c2.Recorder.Enabled || c2.Recorder.RetentionDays != 45 || c2.Recorder.DebounceMs != 5000 {
		t.Fatalf("v2 missing version wiped recorder: %+v", c2)
	}
	if _, err := os.Stat(filepath.Join(dir2, "config.v1.json")); !os.IsNotExist(err) {
		t.Fatalf("v2 missing version should not create config.v1.json, got err=%v", err)
	}

	future := `{"version":5,"language":"en","recorder":{"enabled":false,"retentionDays":60}}`
	s3, dir3 := openFixture(t, future)
	c3 := s3.Get()
	if c3.Version != 5 || c3.Language != "en" || c3.Recorder.Enabled || c3.Recorder.RetentionDays != 60 {
		t.Fatalf("future version not preserved cleanly: %+v", c3)
	}
	if _, err := os.Stat(filepath.Join(dir3, "config.v1.json")); !os.IsNotExist(err) {
		t.Fatalf("future version should not create config.v1.json, got err=%v", err)
	}
}

func TestDuplicateIDsReassignedDeterministically(t *testing.T) {
	s, _ := openFixture(t, `{"version":2,"providers":[{"id":"dup","preset":"openai"},{"id":"dup","preset":"openai"},{"preset":"openai"}]}`)
	ps := s.Get().Providers
	if len(ps) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(ps))
	}
	if ps[0].ID != "dup" || ps[1].ID == "dup" || ps[1].ID == "" || ps[2].ID == "" || ps[1].ID == ps[2].ID {
		t.Fatalf("IDs not unique/deterministic: %+v", ps)
	}
}

func TestAtomicConfigSecretsCommitAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) {
		c.Language = "en"
		c.Providers = []ProviderConfig{{Preset: "openai", ChatBase: "https://old.test/v1", DefaultModel: "gpt-5.2", APIKey: "old-key"}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "secrets.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(c *Config) {
		c.Language = "zh-Hans"
		c.Providers[0].ChatBase = "https://new.test/v1"
		c.Providers[0].APIKey = "new-key"
	})
	if err == nil {
		t.Fatal("expected error when secrets.json.tmp is blocked")
	}
	if got := s.Get(); got.Language != "en" || got.Providers[0].ChatBase != "https://old.test/v1" || got.Providers[0].APIKey != "old-key" {
		t.Fatalf("in-memory config partially mutated: %+v", got)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Get(); got.Language != "en" || got.Providers[0].ChatBase != "https://old.test/v1" || got.Providers[0].APIKey != "old-key" {
		t.Fatalf("on-disk config/secrets mismatched after failed commit: %+v", got)
	}

	_ = os.RemoveAll(filepath.Join(dir, "secrets.json.tmp"))
	cfgNext := s.Get()
	cfgNext.Language = "zh-Hans"
	cfgNext.Providers[0].APIKey = "recovered-key"
	cfgBytes, err := json.MarshalIndent(cfgNext, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	secBytes, err := json.MarshalIndent(map[string]secretEntry{cfgNext.Providers[0].ID: {APIKey: "recovered-key"}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	txBytes, err := json.Marshal(configTx{Config: cfgBytes, Secrets: secBytes})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, txFile), txBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if readOnly.Language != "zh-Hans" {
		t.Fatalf("Read did not observe pending tx config: %+v", readOnly)
	}
	if _, statErr := os.Stat(filepath.Join(dir, txFile)); statErr != nil {
		t.Fatalf("Read removed tx file: %v", statErr)
	}
	recovered, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := recovered.Get(); got.Language != "zh-Hans" || got.Providers[0].APIKey != "recovered-key" {
		t.Fatalf("Open did not replay pending tx: %+v", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, txFile)); !os.IsNotExist(statErr) {
		t.Fatalf("tx file not removed after recovery: %v", statErr)
	}
}

func TestAmbiguousLegacySecretNotDuplicatedAndPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{"version":2,"providers":[{"preset":"openai","label":"shared","chatBase":"https://a.example/v1"},{"preset":"openai","label":"shared","chatBase":"https://b.example/v1"}]}`
	secJSON := `{"openai|shared":{"apiKey":"legacy-shared-key"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(secJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ps := s.Get().Providers
	if len(ps) != 2 || ps[0].APIKey != "" || ps[1].APIKey != "" {
		t.Fatalf("ambiguous legacy secret leaked onto providers: %+v", ps)
	}
	if err := s.Update(func(c *Config) { c.Language = "en" }); err != nil {
		t.Fatal(err)
	}
	rawSec, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil || !strings.Contains(string(rawSec), "legacy-shared-key") {
		t.Fatalf("ambiguous legacy secret dropped on unrelated update: %s %v", rawSec, err)
	}

	if err := s.Update(func(c *Config) { c.Providers[0].APIKey = "explicit-first-key" }); err != nil {
		t.Fatal(err)
	}
	rawSec2, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil || !strings.Contains(string(rawSec2), "legacy-shared-key") || !strings.Contains(string(rawSec2), "explicit-first-key") {
		t.Fatalf("unclaimed legacy secret dropped after configuring first endpoint: %s %v", rawSec2, err)
	}
	reloaded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rp := reloaded.Get().Providers
	if rp[0].APIKey != "explicit-first-key" || rp[1].APIKey != "" {
		t.Fatalf("unexpected keys after claiming first endpoint: %+v", rp)
	}
	if err := reloaded.Update(func(c *Config) { c.Language = "zh-Hans" }); err != nil {
		t.Fatal(err)
	}
	rawSec3, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil || !strings.Contains(string(rawSec3), "legacy-shared-key") {
		t.Fatalf("unclaimed legacy secret dropped after reload update: %s %v", rawSec3, err)
	}
}

func TestConcurrentStoreUpdatesAndStaleV2RejectsUpgradedV3Disk(t *testing.T) {
	dir := t.TempDir()
	s1, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		st := s1
		if i%2 == 1 {
			st = s2
		}
		go func(st *Store, i int) {
			defer wg.Done()
			errs <- st.Update(func(c *Config) {
				if i%2 == 0 {
					c.Language = "en"
				} else {
					c.Language = "zh-Hans"
				}
				c.Providers = []ProviderConfig{{ID: "p1", Preset: "openai", APIKey: "sk-concurrent"}}
			})
		}(st, i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent store Update failed: %v", err)
		}
	}

	v3Raw := `{"version":3,"language":"en","recorder":{"enabled":false},"v3Extra":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(v3Raw), 0o600); err != nil {
		t.Fatal(err)
	}
	err = s1.Update(func(c *Config) { c.Language = "zh-Hans" })
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("stale handle over v3 disk err = %v, want ErrUnsupportedVersion", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(after) != v3Raw {
		t.Fatalf("v3 config on disk overwritten by stale handle: %s %v", after, err)
	}
}

func TestFutureConfigVersionRefusesUpdateWithoutStrippingUnknownFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":5,"language":"en","recorder":{"enabled":false},"futureFeature":{"k":1}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(c *Config) { c.Language = "zh-Hans" })
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Update err = %v, want ErrUnsupportedVersion", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(after) != raw {
		t.Fatalf("future config modified on rejected Update: %s %v", after, err)
	}
}

func TestDeletedAmbiguousLegacySecretNotResurrectedInSameProcess(t *testing.T) {
	dir := t.TempDir()
	cfgJSON := `{"version":2,"providers":[{"preset":"openai","label":"shared","chatBase":"https://a.example/v1"},{"preset":"openai","label":"shared","chatBase":"https://b.example/v1"}]}`
	secJSON := `{"openai|shared":{"apiKey":"legacy-shared-key"}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(secJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	dirPartial := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirPartial, "config.json"), []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPartial, "secrets.json"), []byte(secJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	sp, err := Open(dirPartial)
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Update(func(c *Config) { c.Providers = c.Providers[1:] }); err != nil {
		t.Fatal(err)
	}
	rePartial, err := Open(dirPartial)
	if err != nil {
		t.Fatal(err)
	}
	if rp := rePartial.Get().Providers; len(rp) != 1 || rp[0].APIKey != "" {
		t.Fatalf("quarantined legacy secret auto-assigned after deleting first duplicate: %+v", rp)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) { c.Providers = nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *Config) {
		c.Providers = append(c.Providers, ProviderConfig{Preset: "openai", Label: "shared", ChatBase: "https://c.example/v1"})
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rp := reopened.Get().Providers
	if len(rp) != 1 || rp[0].APIKey != "" {
		t.Fatalf("deleted ambiguous legacy secret resurrected onto new provider: %+v", rp)
	}
	rawSec, err := os.ReadFile(filepath.Join(dir, "secrets.json"))
	if err != nil || strings.Contains(string(rawSec), "legacy-shared-key") {
		t.Fatalf("deleted legacy secret remained on disk: %s %v", rawSec, err)
	}
}

func TestCrossHandleUpdatePreservesOtherHandleProviderAndSecrets(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Update(func(c *Config) {
		c.Providers = []ProviderConfig{{ID: "p-a", Preset: "openai", ChatBase: "https://a.example/v1", APIKey: "secret-a"}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(c *Config) {
		c.Language = "en"
	}); err != nil {
		t.Fatal(err)
	}
	gotB := b.Get()
	if gotB.Language != "en" || len(gotB.Providers) != 1 || gotB.Providers[0].APIKey != "secret-a" {
		t.Fatalf("handle B did not merge disk changes before saving: %+v", gotB)
	}
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	gotC := c.Get()
	if gotC.Language != "en" || len(gotC.Providers) != 1 || gotC.Providers[0].APIKey != "secret-a" {
		t.Fatalf("cross-handle Update lost provider or secret: %+v", gotC)
	}
}

func TestResidualV2TxDoesNotDowngradeV3Config(t *testing.T) {
	dir := t.TempDir()
	staleHandle, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	v3Raw := `{"version":3,"language":"en","recorder":{"enabled":false},"v3Extra":"keep-me"}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(v3Raw), 0o600); err != nil {
		t.Fatal(err)
	}
	txBytes, err := json.Marshal(configTx{
		Config:  []byte(`{"version":2,"language":"zh-Hans","recorder":{"enabled":true}}`),
		Secrets: []byte(`{"stale":{"apiKey":"bad"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, txFile), txBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	ro, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ro.Version != 3 || ro.Language != "en" {
		t.Fatalf("Read replayed stale v2 tx over v3 disk: %+v", ro)
	}

	opened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := opened.Get(); got.Version != 3 || got.Language != "en" {
		t.Fatalf("Open replayed stale v2 tx over v3 disk: %+v", got)
	}
	if err := opened.Update(func(c *Config) { c.Language = "zh-Hans" }); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("opened v3 Update err = %v, want ErrUnsupportedVersion", err)
	}
	if err := staleHandle.Update(func(c *Config) { c.Language = "zh-Hans" }); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("stale v2 Update with residual v2 tx err = %v, want ErrUnsupportedVersion", err)
	}
	diskAfter, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(diskAfter) != v3Raw {
		t.Fatalf("v3 config on disk overwritten by residual v2 tx: %s %v", diskAfter, err)
	}
}

func TestRecreatedProviderGetsNonReusableID(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Update(func(c *Config) {
		c.Providers = []ProviderConfig{{Preset: "openai", Label: "shared", ChatBase: "https://first.example/v1"}}
	}); err != nil {
		t.Fatal(err)
	}
	firstID := a.Get().Providers[0].ID
	if firstID == "" {
		t.Fatal("missing initial provider ID")
	}

	b, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(c *Config) {
		c.Providers = nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Update(func(c *Config) {
		c.Providers = []ProviderConfig{{Preset: "openai", Label: "shared", ChatBase: "https://second.example/v1"}}
	}); err != nil {
		t.Fatal(err)
	}
	secondID := b.Get().Providers[0].ID
	if secondID == "" || secondID == firstID || !strings.HasPrefix(secondID, "p-") {
		t.Fatalf("recreated provider reused old ID or invalid format: first=%q second=%q", firstID, secondID)
	}
}

func TestSeparatedSecretsTxRecoveryAndNoSecretsInTxMeta(t *testing.T) {
	dir := t.TempDir()
	cfgNext := Default()
	cfgNext.Language = "en"
	cfgNext.Providers = []ProviderConfig{{ID: "p-sep", Preset: "openai", ChatBase: "https://sep.test/v1", APIKey: "sk-sep-secret"}}
	cfgBytes, err := json.MarshalIndent(cfgNext, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	secBytes, err := json.MarshalIndent(map[string]secretEntry{"p-sep": {APIKey: "sk-sep-secret"}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(secBytes)
	if err := os.WriteFile(filepath.Join(dir, secretsTxFile), secBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	txBytes, err := json.Marshal(configTx{Config: cfgBytes, SecretsSHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(txBytes), "sk-sep-secret") {
		t.Fatalf("tx metadata contained plaintext/base64 secrets: %s", txBytes)
	}
	if err := os.WriteFile(filepath.Join(dir, txFile), txBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	ro, err := Read(dir)
	if err != nil || ro.Language != "en" || ro.Providers[0].APIKey != "" {
		t.Fatalf("Read failed or leaked secrets: %+v err=%v", ro, err)
	}
	recovered, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := recovered.Get()
	if got.Language != "en" || len(got.Providers) != 1 || got.Providers[0].APIKey != "sk-sep-secret" {
		t.Fatalf("Open did not recover separated secrets tx: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, txFile)); !os.IsNotExist(err) {
		t.Fatalf("tx file remained after recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, secretsTxFile)); !os.IsNotExist(err) {
		t.Fatalf("secrets tx file remained after recovery: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, secretsTxFile), []byte("stale-orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopenedAfterOrphan, err := Open(dir)
	if err != nil {
		t.Fatalf("Open failed with orphan secrets tx after meta removal: %v", err)
	}
	if got := reopenedAfterOrphan.Get(); got.Language != "en" || len(got.Providers) != 1 || got.Providers[0].APIKey != "sk-sep-secret" {
		t.Fatalf("orphan secrets tx corrupted committed config/secrets: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, secretsTxFile)); !os.IsNotExist(err) {
		t.Fatalf("orphan secrets tx file not cleaned on Open: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, txFile), txBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("expected error when tx metadata references missing .secrets.tx")
	}
}
