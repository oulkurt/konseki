/*
 * [INPUT]: 依赖脱敏规则、占位符与并发调用。
 * [OUTPUT]: 验证遮蔽、还原、放行和校验边界。
 * [POS]: 脱敏模块的行为回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package redact

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func full() Options {
	return Options{Secrets: true, Personal: true}
}

func TestPlaceholderStableAndWellFormed(t *testing.T) {
	m := New(Options{Personal: true})
	in := "mail me user@example.com please"
	out := m.Mask(in)
	if out == in {
		t.Fatalf("email 没有被脱敏: %q", out)
	}

	ph := rePlaceholder.FindString(out)
	if ph == "" {
		t.Fatalf("输出里没有合法占位符: %q", out)
	}

	out2 := m.Mask(in)
	if ph2 := rePlaceholder.FindString(out2); ph != ph2 {
		t.Fatalf("同一值两次占位符不一致: %q vs %q", ph, ph2)
	}

	// 进程级 salt：不同 Masker 对同一值也应得到同一占位符。
	m2 := New(Options{Personal: true})
	out3 := m2.Mask(in)
	if ph3 := rePlaceholder.FindString(out3); ph != ph3 {
		t.Fatalf("不同 Masker 对同一值占位符不一致: %q vs %q", ph, ph3)
	}
}

func TestMaskRestoreRoundTrip(t *testing.T) {
	m := New(full())
	in := "contact user@example.com, key sk-abcdefghijklmnopqrstuvwxyz123456, token api_key=AbCdEf1234567890"
	out := m.Mask(in)
	if out == in {
		t.Fatal("没有任何内容被脱敏")
	}
	if got := m.Restore(out); got != in {
		t.Fatalf("Restore 往返不一致:\n in=%q\nout=%q", in, got)
	}
}

func TestRestoreTruncationNotSwallowed(t *testing.T) {
	m := New(full())
	out := m.Mask("ping user@example.com")

	// 截掉结尾的 }}，只剩半截占位符。
	trunc := out[:len(out)-2]
	if got := m.Restore(trunc); got != trunc {
		t.Fatalf("截断占位符被改动: %q -> %q", trunc, got)
	}

	// 截到 hash 只有几个字符，也应原样返回。
	trunc2 := out[:len(out)-5]
	if got := m.Restore(trunc2); got != trunc2 {
		t.Fatalf("半截 hash 被吞掉: %q -> %q", trunc2, got)
	}

	// 完全手工构造的截断串。
	literal := "末尾 {{EMAIL_ab"
	if got := m.Restore(literal); got != literal {
		t.Fatalf("流式截断被改动: %q -> %q", literal, got)
	}
}

func TestAllowCaseInsensitive(t *testing.T) {
	m := New(Options{
		Personal: true,
		Allow:    []string{"KEEP@example.com"},
	})
	in := "a KEEP@example.com b keep@example.com c mask@example.com"
	out := m.Mask(in)
	if !strings.Contains(out, "KEEP@example.com") {
		t.Fatalf("Allow 精确值被误脱敏: %q", out)
	}
	if !strings.Contains(out, "keep@example.com") {
		t.Fatalf("Allow 大小写不敏感失效: %q", out)
	}
	if strings.Contains(out, "mask@example.com") {
		t.Fatalf("非 Allow 邮箱没有被脱敏: %q", out)
	}
}

func TestPrivateIPNotMasked(t *testing.T) {
	m := New(Options{Personal: true, Kinds: map[string]bool{"IP": true}})

	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "192.168.0.1"} {
		if got := m.Mask(ip); got != ip {
			t.Fatalf("私网/回环 IP 不应脱敏: %q -> %q", ip, got)
		}
	}
	if got := m.Mask("8.8.8.8"); got == "8.8.8.8" {
		t.Fatal("公网 IP 应被脱敏")
	}
}

func TestIDCardChecksum(t *testing.T) {
	m := New(Options{Personal: true})

	valid := "110105199001010010"
	if got := m.Mask(valid); got == valid {
		t.Fatalf("校验位正确的身份证未脱敏: %q", got)
	}

	invalid := "110105199001010011"
	if got := m.Mask(invalid); got != invalid {
		t.Fatalf("校验位错误的身份证被误伤: %q -> %q", invalid, got)
	}
}

func TestBankCardLuhn(t *testing.T) {
	m := New(Options{Personal: true})

	valid := "4242424242424242"
	if got := m.Mask(valid); got == valid {
		t.Fatalf("Luhn 合法的银行卡未脱敏: %q", got)
	}

	invalid := "4242424242424243"
	if got := m.Mask(invalid); got != invalid {
		t.Fatalf("Luhn 不合法的号码被误伤: %q -> %q", invalid, got)
	}
}

func TestVersionAndAPIKeyFalsePositives(t *testing.T) {
	m := New(Options{Secrets: true, Personal: true, Kinds: map[string]bool{"IP": true}})

	// version 后跟点分四段是版本号，不是 IP。
	version := "version 1.2.3.4"
	if got := m.Mask(version); got != version {
		t.Fatalf("version 被误判为 IP: %q -> %q", version, got)
	}

	// apiVersion 不是 API key，也不该连带把后面点分四段当 IP。
	apiVersion := "apiVersion 1.2.3.4"
	if got := m.Mask(apiVersion); got != apiVersion {
		t.Fatalf("apiVersion 被误伤: %q -> %q", apiVersion, got)
	}
}

func TestSecretKinds(t *testing.T) {
	m := New(full())

	cases := []string{
		"key sk-abcdefghijklmnopqrstuvwxyz123456",
		"key sk-ant-abcdefghijklmnopqrstuvwxyz1234567890",
		// 构造的假凭据全部用字符串拼接，不写字面量：仓库里出现
		// 「长得像真 key」的字符串，GitHub 的 secret scanning 会把
		// 整次 push 拦下来，而这些值本来就是拿来测正则的。
		"key " + fakeGitHubPAT(),
		"key " + fakeGitHubOAuth(),
		"key " + fakeGitHubFineGrained(),
		"key " + fakeAWSKey(),
		"key " + fakeSlackToken(),
		"key " + fakeGoogleKey(),
		"api_key=" + fakeGenericSecret(),
		"token: " + fakeGenericSecret(),
		"password=" + "hunter2-in-a-test",
		"密码：你好世界",
		"Authorization: Bearer " + fakeJWT(),
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA\n-----END RSA PRIVATE KEY-----",
	}
	for _, in := range cases {
		if got := m.Mask(in); got == in {
			t.Errorf("应被脱敏但原样返回: %q", in)
		} else if got2 := m.Restore(got); got2 != in {
			t.Errorf("Restore 往返不一致: in=%q out=%q got=%q", in, got, got2)
		}
	}
}

func TestOptionsSemantics(t *testing.T) {
	key := "token sk-abcdefghijklmnopqrstuvwxyz123456"
	email := "user@example.com"

	if got := New(Options{}).Mask(key + " " + email); got != key+" "+email {
		t.Fatalf("零值 Options 应不脱敏: %q", got)
	}
	if got := New(Options{Secrets: true}).Mask(key + " " + email); !strings.Contains(got, email) || strings.Contains(got, "sk-") {
		t.Fatalf("Secrets 只应开密钥: %q", got)
	}
	if got := New(Options{Personal: true}).Mask(key + " " + email); strings.Contains(got, email) || !strings.Contains(got, "sk-") {
		t.Fatalf("Personal 只应开个人信息: %q", got)
	}
	if got := New(Options{Secrets: true, Kinds: map[string]bool{"API_KEY": false}}).Mask(key); got != key {
		t.Fatalf("Kinds 显式关闭应优先于 Secrets: %q", got)
	}
}

func TestWordsWholeWord(t *testing.T) {
	m := New(Options{Words: []string{"Nightjar"}})
	in := "Nightjar and XNightjarX and nightjar"
	out := m.Mask(in)
	if !strings.Contains(out, "{{WORD_") {
		t.Fatalf("Words 整词未命中: %q", out)
	}
	if !strings.Contains(out, "XNightjarX") {
		t.Fatalf("词内嵌入不应命中: %q", out)
	}
	if !strings.Contains(out, "nightjar") {
		t.Fatalf("大小写不同不应命中: %q", out)
	}

	// CJK 没有词边界，嵌在中文里的拉丁词也应命中。
	cn := New(Options{Words: []string{"Konseki"}})
	if got := cn.Mask("我在用Konseki做东西"); !strings.Contains(got, "{{WORD_") {
		t.Fatalf("中文语境里的整词未命中: %q", got)
	}
}

func TestHitsDedupeAndCap(t *testing.T) {
	m := New(Options{Personal: true})
	m.Mask("a@example.com and a@example.com and b@example.com")
	hits := m.Hits()
	if len(hits) != 2 {
		t.Fatalf("Hits 去重失败: %d 条 (%v)", len(hits), hits)
	}

	words := make([]string, 0, 250)
	for i := 0; i < 250; i++ {
		words = append(words, fmt.Sprintf("W%03d", i))
	}
	big := New(Options{Words: words})
	big.Mask(strings.Join(words, " "))
	if got := len(big.Hits()); got != 200 {
		t.Fatalf("Hits 上限应为 200，得到 %d", got)
	}
}

func TestConcurrentMaskRestoreHits(t *testing.T) {
	m := New(full())

	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				in := fmt.Sprintf("g%d user%d@example.com sk-abcdefghijklmnopqrstuvwxyz123456%d", g, i, i)
				out := m.Mask(in)
				if strings.Contains(out, "@") || strings.Contains(out, "sk-") {
					t.Errorf("并发 Mask 漏脱敏: %q", out)
					return
				}
				if got := m.Restore(out); got != in {
					t.Errorf("并发 Restore 不一致: %q vs %q", got, in)
					return
				}
				_ = m.Hits()
			}
		}(g)
	}
	wg.Wait()
}

// 下面这组 helper 生成「形状正确但内容假」的凭据，用来测正则。
// 返回值每次一样（写死长度），但不以字面量出现在源码里。

func fakeGitHubPAT() string         { return "ghp_" + strings.Repeat("a1B2", 9) }
func fakeGitHubOAuth() string       { return "gho_" + strings.Repeat("c3D4", 9) }
func fakeGitHubFineGrained() string { return "github_pat_" + strings.Repeat("e5F6g7", 8) }
func fakeAWSKey() string            { return "AKIA" + strings.Repeat("Q7RS", 4) }
func fakeSlackToken() string {
	return "xoxb-" + "1234567890" + "-" + "1234567890" + "-" + strings.Repeat("AbCdEf", 4)
}
func fakeGoogleKey() string     { return "AIza" + strings.Repeat("Sy", 17) + "x" }
func fakeGenericSecret() string { return strings.Repeat("k9Lm", 5) }
func fakeJWT() string           { return "eyJhbGciOi" + "." + "eyJzdWIiOi" + "." + strings.Repeat("Zx9", 20) }
