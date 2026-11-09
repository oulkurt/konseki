/*
 * [INPUT]: 依赖逐拍累计读数、隐私状态和会话边界。
 * [OUTPUT]: 提供快照与会话的键鼠计数桶。
 * [POS]: 纯计数逻辑；Runner 持锁调用，不访问平台或磁盘。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

type countBucket struct {
	keys, clicks int
	valid        bool
}

func (b countBucket) values() (*int, *int) {
	if !b.valid {
		return nil, nil
	}
	return new(b.keys), new(b.clicks)
}

type inputCounts struct {
	last              InputTotals
	have, private     bool
	snapshot, session countBucket
}

// invalidate 在开关变化或读数失败时中断当前区间。
func (c *inputCounts) invalidate() {
	c.have = false
	c.snapshot.valid = false
	c.session.valid = false
}

// open 在会话打开时清空两个桶。开着计数且已有上一拍读数，区间才从开头被覆盖，桶才有效。
// 本拍读数失败时，调用方已先 invalidate，have 为 false。
func (c *inputCounts) open(enabled bool) {
	valid := enabled && c.have
	c.snapshot = countBucket{valid: valid}
	c.session = countBucket{valid: valid}
}

func (c *inputCounts) observe(enabled, ok bool, cur InputTotals, private, active bool) {
	if !enabled || !ok {
		c.invalidate()
		c.private = private
		return
	}
	if c.have && !private && !c.private && active && cur.Keys >= c.last.Keys && cur.Clicks >= c.last.Clicks {
		keys, clicks := int(cur.Keys-c.last.Keys), int(cur.Clicks-c.last.Clicks)
		if cur.Secure || c.last.Secure {
			keys = 0
		}
		c.snapshot.keys += keys
		c.snapshot.clicks += clicks
		c.session.keys += keys
		c.session.clicks += clicks
	}
	c.last, c.have, c.private = cur, true, private
}

func (c *inputCounts) takeSnapshot(enabled bool) countBucket {
	b := c.snapshot
	c.snapshot = countBucket{valid: enabled && c.have}
	return b
}

func (c *inputCounts) restoreSnapshot(b countBucket) {
	c.snapshot.keys += b.keys
	c.snapshot.clicks += b.clicks
	c.snapshot.valid = c.snapshot.valid && b.valid
}
