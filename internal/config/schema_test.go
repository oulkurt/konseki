/*
 * [INPUT]: 依赖临时设置文件与配置存储。
 * [OUTPUT]: 验证迁移、默认值和归一化边界。
 * [POS]: 配置模块的磁盘契约测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
			if _, err = Open(dir); err != nil {
				t.Fatal(err)
			}
			b, _ = os.ReadFile(backup)
			if string(b) != original {
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
