/*
 * [INPUT]: 依赖 regexp、sort、strconv、strings 和 unicode/utf8。
 * [OUTPUT]: 提供按 kind 扫描敏感区间的 matcher、边界校验和校验和检查。
 * [POS]: 脱敏层的规则实现。redact.go 负责占位符和反查表，这里只找区间。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package redact

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// span 是文本里一段待脱敏的区间。start/end 是字节下标。
type span struct {
	kind  string
	start int
	end   int
}

// matcher 是单条规则的扫描器，返回该规则命中的所有候选区间。
// 边界校验放在每个 scan 内部，因为不同 kind 的「合法邻居」不一样。
type matcher struct {
	kind string
	scan func(s string) []span
}

// buildRules 只把启用的 kind 编进规则表。顺序即优先级：
// 排在前面的规则在区间重叠时先被保留（见 nonOverlapping）。
func buildRules(enabled map[string]bool, words []string) []matcher {
	var out []matcher

	add := func(kind string, m matcher) {
		if enabled[kind] {
			out = append(out, m)
		}
	}

	add("PRIVATE_KEY", regexMatcher("PRIVATE_KEY", rePrivateKey, func(s string, a, b int) bool {
		// 防止 ------BEGIN 这类被识别成块头。
		return a == 0 || s[a-1] != '-'
	}))
	add("SECRET", matcher{kind: "SECRET", scan: secretScan})
	add("PASSWORD", matcher{kind: "PASSWORD", scan: passwordScan})
	add("BEARER", matcher{kind: "BEARER", scan: bearerScan})
	add("API_KEY", regexMatcher("API_KEY", reAPIKey, func(s string, a, b int) bool {
		return boundOK(s, a, b, isTokenChar)
	}))
	add("EMAIL", regexMatcher("EMAIL", reEmail, emailOK))
	add("PHONE", regexMatcher("PHONE", rePhone, func(s string, a, b int) bool {
		return boundOK(s, a, b, isDigit)
	}))
	add("ID_CARD", regexMatcher("ID_CARD", reIDCard, func(s string, a, b int) bool {
		return boundOK(s, a, b, isAlnum) && idCardOK(s[a:b])
	}))
	add("BANK_CARD", matcher{kind: "BANK_CARD", scan: bankCardScan})
	add("IP", regexMatcher("IP", reIP, ipOK))
	add("HOME", regexMatcher("HOME", reHome, homeOK))
	add("MAC", regexMatcher("MAC", reMAC, macOK))

	if len(words) > 0 {
		out = append(out, wordMatcher(words))
	}
	return out
}

// regexMatcher 把一条正则规则包成 matcher；ok 为 nil 表示只信正则本身。
func regexMatcher(kind string, re *regexp.Regexp, ok func(s string, a, b int) bool) matcher {
	return matcher{
		kind: kind,
		scan: func(s string) []span {
			var out []span
			for _, m := range re.FindAllStringIndex(s, -1) {
				a, b := m[0], m[1]
				if ok != nil && !ok(s, a, b) {
					continue
				}
				out = append(out, span{kind: kind, start: a, end: b})
			}
			return out
		},
	}
}

// nonOverlapping 把可能重叠的候选区间压成互不重叠的一段段：
// 起点相同取更长，重叠时保留先开始的那条。
func nonOverlapping(spans []span) []span {
	// 用稳定排序：区间完全相同时保留 buildRules 里靠前规则的优先级，
	// 例如 18 位号码同时像身份证和银行卡时，优先按身份证处理。
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end > spans[j].end
	})

	out := make([]span, 0, len(spans))
	lastEnd := -1
	for _, sp := range spans {
		if sp.start < lastEnd {
			continue
		}
		out = append(out, sp)
		lastEnd = sp.end
	}
	return out
}

// 各类规则的预编译正则。全部是「宁可漏不误伤」：前缀要有辨识度，
// 长度给下限，边界字符单独再验一遍。

var rePrivateKey = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----[\s\S]*?-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)

var reAPIKey = regexp.MustCompile(`(?:sk-[A-Za-z0-9_-]{16,}|gh[op]_[A-Za-z0-9]{36}|github_pat_[A-Za-z0-9_]{22,}|AKIA[0-9A-Z]{16}|xoxb-[0-9]{10,}-[0-9]{10,}-[A-Za-z0-9-]{20,}|AIza[0-9A-Za-z_-]{35})`)

var reSecret = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_])(api[_-]?key|token|secret)[ \t]*[:=][ \t]*([A-Za-z0-9_\-\./=+@]+)`)

var rePassword = regexp.MustCompile(`(?i)(?:password|passwd|pwd|密码|密碼|パスワード|비밀번호)[ \t]*[:=：][ \t]*([^\s,;，。；'"<>()\[\]{}]+)`)

var reBearer = regexp.MustCompile(`(?i)Bearer[ \t]+([A-Za-z0-9._~+/=-]{8,})`)

var reEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)

var rePhone = regexp.MustCompile(`(?:\+?86[- ]?)?1[3-9]\d{9}`)

var reIDCard = regexp.MustCompile(`[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[0-9Xx]`)

var reBankCard = regexp.MustCompile(`\d(?:[ -]?\d){12,18}`)

var reIP = regexp.MustCompile(`(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}`)

var reHome = regexp.MustCompile(`(?:~|/Users/[A-Za-z0-9._-]+|/home/[A-Za-z0-9._-]+|[A-Za-z]:\\Users\\[A-Za-z0-9._-]+)(?:[/\\][A-Za-z0-9._-]+)*`)

var reMAC = regexp.MustCompile(`(?:[0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}`)

// secretScan 只替换赋值号右边那截值，字段名保留，这样日志仍可读。
func secretScan(s string) []span {
	var out []span
	for _, m := range reSecret.FindAllStringSubmatchIndex(s, -1) {
		if m[4] < 0 || m[5] <= m[4] {
			continue
		}
		out = append(out, span{kind: "SECRET", start: m[4], end: m[5]})
	}
	return out
}

// passwordScan 支持 password= 与 密码：两种写法。值里允许中文，
// 所以用「非空白非分隔符」而不是 token 字符集。
func passwordScan(s string) []span {
	var out []span
	for _, m := range rePassword.FindAllStringSubmatchIndex(s, -1) {
		// 捕获组可能只有 1 个（索引 2/3），也可能是 2 个（4/5）。
		// 取最后一个存在的组，避免规则表微调就 panic。
		if len(m) >= 6 && m[4] >= 0 {
			out = append(out, span{kind: "PASSWORD", start: m[4], end: m[5]})
			continue
		}
		if len(m) >= 4 && m[2] >= 0 && m[3] > m[2] {
			out = append(out, span{kind: "PASSWORD", start: m[2], end: m[3]})
		}
	}
	return out
}

// bearerScan 只替换 Bearer 后面的 token，保留 scheme 本身。
func bearerScan(s string) []span {
	var out []span
	for _, m := range reBearer.FindAllStringSubmatchIndex(s, -1) {
		if m[2] < 0 || m[3] <= m[2] {
			continue
		}
		// 防止 xBearer 这类长词里的 Bearer 被单独命中。
		if m[0] > 0 && isAlnum(s[m[0]-1]) {
			continue
		}
		out = append(out, span{kind: "BEARER", start: m[2], end: m[3]})
	}
	return out
}

// bankCardScan 把可能带空格/连字符的数字串规整后做 Luhn 校验。
func bankCardScan(s string) []span {
	var out []span
	for _, m := range reBankCard.FindAllStringIndex(s, -1) {
		a, b := m[0], m[1]
		if !boundOK(s, a, b, isDigit) {
			continue
		}
		digits := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return -1
			}
			return r
		}, s[a:b])
		if len(digits) < 13 || len(digits) > 19 || !luhnOK(digits) {
			continue
		}
		out = append(out, span{kind: "BANK_CARD", start: a, end: b})
	}
	return out
}

func emailOK(s string, a, b int) bool {
	if a > 0 {
		c := s[a-1]
		// local 部分前面不能再有邮箱可用字符，否则是更长标识符的一部分。
		if isAlnum(c) || c == '.' || c == '_' || c == '%' || c == '+' || c == '-' {
			return false
		}
	}
	if b < len(s) {
		c := s[b]
		// TLD 后面不能再接字母/数字/连字符，否则 a@b.comx 会被整体吃掉。
		if isAlnum(c) || c == '-' {
			return false
		}
	}

	val := s[a:b]
	at := strings.IndexByte(val, '@')
	if at <= 0 {
		return false
	}
	local, domain := val[:at], val[at+1:]
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	for _, p := range strings.Split(domain, ".") {
		if p == "" || strings.HasPrefix(p, "-") || strings.HasSuffix(p, "-") {
			return false
		}
	}
	return true
}

func ipOK(s string, a, b int) bool {
	// 字母数字都挡：1.2.3.4 嵌在 foo1.2.3.4 里同样不算独立 IP。
	if !boundOK(s, a, b, isAlnum) {
		return false
	}
	if !ipContextOK(s, a) {
		return false
	}
	return !privateIP(s[a:b])
}

// ipContextOK 挡住 “version 1.2.3.4” 这类版本号：往前找最近一个纯字母词，
// 是 version/v（或以 version 结尾，如 apiVersion）就不当 IP。
func ipContextOK(s string, a int) bool {
	i := a - 1
	for i >= 0 && !isAlnum(s[i]) {
		i--
	}
	if i < 0 {
		return true
	}
	j := i
	for j >= 0 && isAlpha(s[j]) {
		j--
	}
	w := strings.ToLower(s[j+1 : i+1])
	return w != "v" && w != "version" && !strings.HasSuffix(w, "version")
}

// privateIP 只放行真正的私网/回环地址：127/8、10/8、192.168/16。
// 这些不是个人信息，宁可漏掉也不要把环境地址当敏感值。
func privateIP(s string) bool {
	p := strings.Split(s, ".")
	if len(p) != 4 {
		return false
	}
	a, err1 := strconv.Atoi(p[0])
	b, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return a == 127 || a == 10 || (a == 192 && b == 168)
}

func homeOK(s string, a, b int) bool {
	return boundOK(s, a, b, func(c byte) bool {
		return isAlnum(c) || c == '_' || c == '-' || c == '.' || c == '~'
	})
}

func macOK(s string, a, b int) bool {
	return boundOK(s, a, b, func(c byte) bool {
		return isHexChar(c) || c == ':' || c == '-'
	})
}

// wordMatcher 把 Options.Words 当 WORD kind 整词匹配。
func wordMatcher(words []string) matcher {
	return matcher{
		kind: "WORD",
		scan: func(s string) []span {
			var out []span
			for _, w := range words {
				if w == "" {
					continue
				}
				from := 0
				for from <= len(s) {
					i := strings.Index(s[from:], w)
					if i < 0 {
						break
					}
					a := from + i
					b := a + len(w)
					if wordBoundaryOK(s, a, b) {
						out = append(out, span{kind: "WORD", start: a, end: b})
					}
					from = a + len(w)
				}
			}
			return out
		},
	}
}

// wordBoundaryOK 判断一个词是否独立。
//
// 只把 ASCII 字母数字和下划线当作「词内字符」。中文没有分词边界，
// 如果把 CJK 也算进去，「我在用Konseki做东西」里的 Konseki 会被判成
// 词中而漏遮——那恰恰是用户最希望遮掉的形态。
func wordBoundaryOK(s string, a, b int) bool {
	if a > 0 {
		if r, _ := utf8.DecodeLastRuneInString(s[:a]); isWordRune(r) {
			return false
		}
	}
	if b < len(s) {
		if r, _ := utf8.DecodeRuneInString(s[b:]); isWordRune(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// 字节级字符分类。规则都工作在 ASCII 边界上，避免每个候选都走 utf8。
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isAlnum(c byte) bool { return isAlpha(c) || isDigit(c) }
func isTokenChar(c byte) bool {
	return isAlnum(c) || c == '_' || c == '-'
}
func isHexChar(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// boundOK 检查区间两侧是否都落在允许的「邻居」之外。pred 返回 true
// 表示这个字符不允许出现在边界上。
func boundOK(s string, a, b int, pred func(byte) bool) bool {
	if a > 0 && pred(s[a-1]) {
		return false
	}
	if b < len(s) && pred(s[b]) {
		return false
	}
	return true
}

// 身份证校验位：GB 11643-1999 的加权和，取模后查表。
var idWeights = []int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}

const idCheckChars = "10X98765432"

func idCardOK(s string) bool {
	if len(s) != 18 {
		return false
	}
	y, err1 := strconv.Atoi(s[6:10])
	m, err2 := strconv.Atoi(s[10:12])
	d, err3 := strconv.Atoi(s[12:14])
	if err1 != nil || err2 != nil || err3 != nil || !validDate(y, m, d) {
		return false
	}

	sum := 0
	for i := 0; i < 17; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		sum += int(c-'0') * idWeights[i]
	}
	last := s[17]
	if last == 'x' {
		last = 'X'
	}
	return last == idCheckChars[sum%11]
}

func validDate(y, m, d int) bool {
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	if m == 2 && (y%4 == 0 && y%100 != 0 || y%400 == 0) {
		return d <= 29
	}
	return d <= days[m-1]
}

// luhnOK 是银行卡号的标准 Luhn 校验。
func luhnOK(digits string) bool {
	sum := 0
	n := len(digits)
	for i := 0; i < n; i++ {
		d := int(digits[n-1-i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
