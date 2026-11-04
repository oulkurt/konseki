package providers

import "testing"

// 这些测试是目录表的基础契约：任何新增/删除预设都不能破坏它们。
// 之所以把「id 唯一」「Models 非空」放进测试而不是靠人工检查，
// 是因为这张表会频繁增删，坏数据会在 CI 里立刻暴露。

func TestAllNonEmptyAndIDsUnique(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned no presets")
	}
	if len(all) < 30 {
		t.Fatalf("expected at least 30 presets, got %d", len(all))
	}

	seen := make(map[string]bool, len(all))
	for _, p := range all {
		if p.ID == "" {
			t.Fatal("found a preset with an empty ID")
		}
		if seen[p.ID] {
			t.Fatalf("duplicate preset ID %q", p.ID)
		}
		seen[p.ID] = true
	}
}

func TestByID(t *testing.T) {
	p, ok := ByID("anthropic")
	if !ok {
		t.Fatal("ByID(anthropic) should hit")
	}
	if p.ID != "anthropic" {
		t.Fatalf("ByID returned wrong preset: got ID %q", p.ID)
	}

	if _, ok := ByID("no-such-preset"); ok {
		t.Fatal("ByID(no-such-preset) should miss")
	}
}

func TestModelsNonEmpty(t *testing.T) {
	// 每个预设都必须给出静态模型列表，即使厂商支持动态拉取，
	// 因为 Models(id) 是调用方在不联网时的兜底。
	for _, p := range All() {
		if got := Models(p.ID); len(got) == 0 {
			t.Errorf("Models(%q) returned no models", p.ID)
		}
	}
}

func TestDetectLocalDoesNotPanic(t *testing.T) {
	// DetectLocal 会去碰本机真实文件/钥匙串，这里只断言它不 panic。
	// 测试机可能是未登录状态，所以不假设结果非空。
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DetectLocal panicked: %v", r)
		}
	}()
	_ = DetectLocal()
}
