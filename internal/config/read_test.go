/*
 * [INPUT]: 依赖临时设置目录与脱敏规则类型。
 * [OUTPUT]: 验证 Read 只读、旧配置迁移不落盘、脱敏总开关的规则翻译。
 * [POS]: 配置模块只读接口和脱敏语义的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"konseki/internal/redact"
)

type fileState struct {
	Size    int64
	Mode    os.FileMode
	ModTime int64
}

func dirState(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]fileState{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = fileState{info.Size(), info.Mode(), info.ModTime().UnixNano()}
	}
	return out
}

func TestReadDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"providers":[{"preset":"p","label":"l"}],"summary":{"language":"en"},"skill":{"exportDir":"/tmp/old","includeOCR":true,"redactOnExport":true,"days":3}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(`{"p|l":{"apiKey":"secret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := dirState(t, dir)

	cfg, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Language != "en" || cfg.Providers[0].APIKey != "" {
		t.Fatalf("读取结果: %+v", cfg)
	}
	if cfg.Skill.ExportDir != "/tmp/old" {
		t.Fatalf("旧导出目录 = %q", cfg.Skill.ExportDir)
	}
	if after := dirState(t, dir); !reflect.DeepEqual(before, after) {
		t.Fatalf("Read 写入了文件: %#v -> %#v", before, after)
	}
}

func TestReadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Fatal("文件不存在时没有返回默认值")
	}
}

func TestRedactOptionsHonorMasterSwitch(t *testing.T) {
	off := RedactConfig{
		Enabled: false, Secrets: true, Personal: true,
		Kinds: map[string]bool{"EMAIL": true}, Allow: []string{"allowed@example.com"}, Words: []string{"项目代号"},
	}
	if got := off.Options(); !reflect.DeepEqual(got, redact.Options{}) {
		t.Fatalf("总开关关闭时仍返回规则: %+v", got)
	}
	on := RedactConfig{
		Enabled: true, Secrets: true, Personal: false,
		Kinds: map[string]bool{"IP": true}, Allow: []string{"allowed@example.com"}, Words: []string{"项目代号"},
	}
	want := redact.Options{
		Secrets: true, Personal: false,
		Kinds: map[string]bool{"IP": true}, Allow: []string{"allowed@example.com"}, Words: []string{"项目代号"},
	}
	if got := on.Options(); !reflect.DeepEqual(got, want) {
		t.Fatalf("规则翻译 = %+v，期望 %+v", got, want)
	}
}
