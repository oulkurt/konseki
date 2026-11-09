/*
 * [INPUT]: 依赖人工构造的累计读数和隐私状态。
 * [OUTPUT]: 验证键鼠计数区间、隐私和读数边界。
 * [POS]: 纯计数逻辑的回归测试，不访问平台。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import "testing"

func TestCountsPrivacySecureAndReset(t *testing.T) {
	var c inputCounts
	tick := func(k, click uint32, secure, private bool) {
		c.observe(true, true, InputTotals{Keys: k, Clicks: click, Secure: secure}, private, true)
	}
	tick(10, 10, false, false)
	c.open(true)
	tick(12, 11, false, false)
	tick(20, 12, false, true)
	tick(30, 13, false, false)
	tick(32, 14, true, false)
	tick(34, 15, false, false)
	tick(35, 16, false, false)
	tick(1, 1, false, false)
	tick(3, 2, false, false)
	if c.session.keys != 5 || c.session.clicks != 5 {
		t.Fatalf("计数错误: %+v", c.session)
	}
}

func TestCountsCoverageAndRestore(t *testing.T) {
	var c inputCounts
	c.observe(true, true, InputTotals{}, false, false)
	c.open(true)
	c.observe(true, true, InputTotals{Keys: 2}, false, true)
	saved := c.takeSnapshot(true)
	c.observe(true, true, InputTotals{Keys: 3}, false, true)
	c.restoreSnapshot(saved)
	if c.snapshot.keys != 3 || !c.snapshot.valid {
		t.Fatalf("回补失败: %+v", c.snapshot)
	}
	c.observe(false, false, InputTotals{}, false, true)
	c.observe(true, true, InputTotals{Keys: 10}, false, true)
	if c.session.valid || c.snapshot.valid {
		t.Fatal("开关中断后区间仍有效")
	}
	c.takeSnapshot(true)
	if !c.snapshot.valid {
		t.Fatal("完整新区间应有效")
	}
	c.observe(true, false, InputTotals{}, false, true)
	if c.snapshot.valid || c.session.valid {
		t.Fatal("读数失败后区间仍有效")
	}
	if k, v := (countBucket{valid: true}).values(); k == nil || *k != 0 || v == nil || *v != 0 {
		t.Fatal("零值必须保留")
	}
}
