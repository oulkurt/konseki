/*
 * [INPUT]: 依赖 crypto/hmac、SHA-256 与进程级随机 salt。
 * [OUTPUT]: 提供 Options、Categories、New、Masker.Mask、Masker.Restore 和 Masker.Hits。
 * [POS]: 脱敏层的入口。规则扫描在 rules.go，调用方是摘要生成器和只读查询。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package redact 在文本离开本机之前，把不该出门的值换成可逆占位符。
//
// 为什么需要这一层：Konseki 会截屏 OCR，屏幕上什么都有——别人发来的
// 邮箱、正在编辑的 .env、GitHub 的 token。发给模型供应商之前必须先把
// 这些值遮掉，发出去就收不回来。
//
// 为什么用占位符而不是直接删掉：模型需要句子的结构。把
// `KEY=sk-ant-xxx` 换成 `KEY={{API_KEY_k3v9x2mq}}`，句子结构还在，
// 模型能正常理解；返回的文本再换回原文，用户看到的仍是自己的内容。
//
// 同一 (kind, value) 必须换出同一个占位符：同一段内容在一轮对话里会
// 出现很多次，每次都不一样会让模型 cache 失效，还会让它以为是不同的
// 东西。占位符用 HMAC-SHA256 计算，key 是进程级随机 salt，所以进程内
// 稳定、跨进程不可预测，反查表只留在内存里。
package redact

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Options 决定一个 Masker 启用哪些规则、放行哪些值。
type Options struct {
	// Secrets 开密钥与凭证：API_KEY/SECRET/PASSWORD/PRIVATE_KEY/BEARER。
	Secrets bool
	// Personal 开个人信息；IP/HOME/MAC 也在 Personal 之下，但默认关，
	// 要用的话在 Kinds 里显式打开。
	Personal bool
	// Kinds 覆盖单个 kind 或整个类别的开关，大小写不敏感，优先级最高。
	Kinds map[string]bool
	// Words 是用户自己的词，整词命中记为 WORD kind。
	Words []string
	// Allow 是明确放行的值，整值匹配、大小写不敏感。
	Allow []string
}

// Category 描述一组 kind，On 是组开关打开时的默认状态。
type Category struct {
	ID    string
	Name  string
	Kinds []string
	On    bool
}

// Categories 是内置类别声明。默认开/关记录在 On 上：
// 密钥与个人身份信息默认开，网络与设备（IP/HOME/MAC）默认关。
var Categories = []Category{
	{
		ID:    "secrets",
		Name:  "密钥与凭证",
		Kinds: []string{"API_KEY", "SECRET", "PASSWORD", "PRIVATE_KEY", "BEARER"},
		On:    true,
	},
	{
		ID:    "personal",
		Name:  "个人身份信息",
		Kinds: []string{"EMAIL", "PHONE", "ID_CARD", "BANK_CARD"},
		On:    true,
	},
	{
		ID:    "network",
		Name:  "网络与设备",
		Kinds: []string{"IP", "HOME", "MAC"},
		On:    false,
	},
}

// Hit 是一次命中：哪个 kind 命中了哪个值。
type Hit struct {
	Kind  string
	Value string
}

const (
	maxHits  = 200
	maxTable = 10000
)

// rePlaceholder 匹配完整占位符。截断的半截（如 {{EMAIL_ab）不匹配，
// Restore 因此会把它原样留下而不是吞掉。
var rePlaceholder = regexp.MustCompile(`\{\{[A-Z][A-Z0-9_]*_[a-z2-7]{8}\}\}`)

var (
	saltOnce sync.Once
	salt     []byte
)

// b32 是无 padding 的标准 base32，小写后取前 8 位作为占位符尾缀。
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Masker 持有一次配置下的规则与命中记录，可安全并发使用。
type Masker struct {
	enabled map[string]bool // kind -> 是否启用（New 时算好，之后只读）
	words   []string
	allow   []string
	rules   []matcher

	mu         sync.RWMutex
	hits       []Hit
	hitSet     map[string]struct{}
	table      map[string]string
	tableOrder []string
	tableHead  int
}

// New 按 Options 预编译规则，返回可复用的 Masker。
func New(opts Options) *Masker {
	m := &Masker{
		enabled: map[string]bool{},
		words:   slices.Clone(opts.Words),
		allow:   slices.Clone(opts.Allow),
		hitSet:  map[string]struct{}{},
		table:   map[string]string{},
	}

	keys := make([]string, 0, len(opts.Kinds))
	for k := range opts.Kinds {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	norm := make(map[string]bool, len(opts.Kinds))
	for _, k := range keys {
		norm[strings.ToUpper(k)] = opts.Kinds[k]
	}

	for _, cat := range Categories {
		for _, kind := range cat.Kinds {
			if kindEnabled(kind, cat, opts, norm) {
				m.enabled[kind] = true
			}
		}
	}
	if v, ok := norm["WORD"]; ok {
		m.enabled["WORD"] = v
	} else if len(m.words) > 0 {
		m.enabled["WORD"] = true
	}

	m.rules = buildRules(m.enabled, m.words)
	return m
}

// kindEnabled 判定单个 kind 是否启用。显式 Kinds 覆盖优先，其次才是
// Secrets/Personal 组开关和类别默认值。
func kindEnabled(kind string, cat Category, o Options, norm map[string]bool) bool {
	if v, ok := norm[kind]; ok {
		return v
	}
	if v, ok := norm[strings.ToUpper(cat.ID)]; ok {
		return v
	}
	if cat.ID == "secrets" {
		return o.Secrets
	}
	return o.Personal && cat.On
}

// Mask 找到并替换 text 里的敏感值，返回脱敏后的文本。
func (m *Masker) Mask(text string) string {
	if text == "" || len(m.rules) == 0 {
		return text
	}

	spans := m.findSpans(text)
	if len(spans) == 0 {
		return text
	}
	spans = nonOverlapping(spans)

	var b strings.Builder
	b.Grow(len(text) + len(spans)*8)
	last := 0
	for _, sp := range spans {
		val := text[sp.start:sp.end]
		if m.allowed(val) {
			// Allow 整值放行：不写占位符，靠最后一段补回原文。
			continue
		}
		b.WriteString(text[last:sp.start])
		b.WriteString(m.placeholderFor(sp.kind, val))
		last = sp.end
		m.recordHit(sp.kind, val)
	}
	b.WriteString(text[last:])
	return b.String()
}

// Restore 把完整占位符换回原值。截断的半截占位符不匹配正则，原样保留。
func (m *Masker) Restore(text string) string {
	if len(m.rules) == 0 || text == "" {
		return text
	}
	return rePlaceholder.ReplaceAllStringFunc(text, func(ph string) string {
		m.mu.RLock()
		v, ok := m.table[ph]
		m.mu.RUnlock()
		if !ok {
			return ph
		}
		return v
	})
}

// Hits 返回去重后的命中记录（最多 200 条），顺序为首次出现顺序。
func (m *Masker) Hits() []Hit {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Hit, len(m.hits))
	copy(out, m.hits)
	return out
}

func (m *Masker) placeholderFor(kind, val string) string {
	h := hmac.New(sha256.New, saltBytes())
	h.Write([]byte(kind + "\x00" + val))
	sum := h.Sum(nil)
	ph := "{{" + kind + "_" + strings.ToLower(b32.EncodeToString(sum)[:8]) + "}}"

	m.mu.Lock()
	if _, exists := m.table[ph]; !exists {
		if len(m.table) >= maxTable {
			evict := m.tableOrder[m.tableHead]
			delete(m.table, evict)
			m.tableOrder[m.tableHead] = ph
			m.tableHead = (m.tableHead + 1) % maxTable
		} else {
			m.tableOrder = append(m.tableOrder, ph)
		}
		m.table[ph] = val
	}
	m.mu.Unlock()
	return ph
}

// saltBytes 惰性生成 32 字节进程级随机 salt，只生成一次。
func saltBytes() []byte {
	saltOnce.Do(func() {
		salt = make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			// crypto/rand 在正常系统上不会失败；兜底只是防极端环境。
			for i := range salt {
				salt[i] = byte(time.Now().UnixNano() >> ((i % 8) * 8))
			}
		}
	})
	return salt
}

// allowed 判断 value 是否整值命中 Allow（大小写不敏感）。
func (m *Masker) allowed(val string) bool {
	for _, a := range m.allow {
		if strings.EqualFold(a, val) {
			return true
		}
	}
	return false
}

func (m *Masker) findSpans(s string) []span {
	var out []span
	for _, r := range m.rules {
		out = append(out, r.scan(s)...)
	}
	return out
}

func (m *Masker) recordHit(kind, val string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.hits) >= maxHits {
		return
	}
	key := kind + "\x00" + val
	if _, ok := m.hitSet[key]; ok {
		return
	}
	m.hitSet[key] = struct{}{}
	m.hits = append(m.hits, Hit{Kind: kind, Value: val})
}
