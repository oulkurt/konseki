/*
 * [INPUT]: 依赖设置、采集、摘要和脱敏的只读接口。
 * [OUTPUT]: 提供 Agent 用的 Main 命令入口和 Env。
 * [POS]: 只读查询命令入口。链接写入由 link.go 提供。
 * 每条命令输出一个 JSON 文档。不建目录、不写文件、不读密钥。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
// Package query 是 Konseki 给其他 Agent 的只读命令。
//
// App 每次启动时在数据目录写一个指向自身可执行文件的软链接，
// Agent 运行 `konseki query ...` 就能读到摘要、时间块、会话、
// 窗口标题、屏幕文字和截图，而不会打开窗口。
//
// 全部命令只读：不建目录、不写文件、不读 secrets.json，
// 也不输出供应商配置、脱敏词表和放行表。
package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"konseki/internal/capture"
	"konseki/internal/config"
	"konseki/internal/redact"
	"konseki/internal/summary"
)

// Env 是一次查询的运行环境。Dir 是 Konseki 的数据目录。
type Env struct {
	Dir string
	Now func() time.Time
}

// LinkName 是数据目录里指向 App 可执行文件的软链接名。
const LinkName = "konseki"

const usage = `usage: konseki query <command> [flags]

commands:
  status                 data folders, recording state, redaction settings
  days [--limit N]       days with data, newest first
  day [--date D]         summaries, time blocks and sessions of one day
  snapshots [--date D] [--from HH:MM] [--to HH:MM] [--app NAME] [--ocr] [--limit N]
  search TEXT [--days N] [--limit N]

D is YYYY-MM-DD, today or yesterday. Output is JSON.`

// Main 执行一条查询命令并返回退出码。
func Main(args []string, env Env, stdout, stderr io.Writer) int {
	if env.Now == nil {
		env.Now = time.Now
	}
	help := len(args) == 1 && args[0] == "help"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if a == "-h" || a == "--help" {
			help = true
			break
		}
		if strings.HasPrefix(a, "--") && valueFlags[strings.TrimPrefix(a, "--")] {
			i++
		}
	}
	if help {
		fmt.Fprintln(stdout, usage)
		return 0
	}

	cmd, opts, err := parseArgs(args)
	if err != nil {
		return printUsageError(stderr, err)
	}
	if cmd == "" {
		return printUsageError(stderr, errors.New("missing command"))
	}
	if _, err := os.Stat(env.Dir); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "konseki query: no Konseki data at %s. Open Konseki once first.\n", env.Dir)
		return 1
	} else if err != nil {
		return fail(stderr, err)
	}

	cfg, err := config.Read(env.Dir)
	if err != nil {
		return fail(stderr, err)
	}
	store, err := capture.OpenStore(cfg.Recorder.Dir(env.Dir))
	if err != nil {
		return fail(stderr, err)
	}
	r := &runner{
		env:  env,
		cfg:  cfg,
		cap:  store,
		sums: summary.OpenStore(env.Dir),
		mask: redact.New(cfg.Redact.Options()),
	}
	switch cmd {
	case "status":
		err = r.status(stdout)
	case "days":
		err = r.days(stdout, opts)
	case "day":
		err = r.day(stdout, opts)
	case "snapshots":
		err = r.snapshots(stdout, opts)
	case "search":
		err = r.search(stdout, opts)
	}
	if err != nil {
		if ue, ok := err.(usageError); ok {
			return printUsageError(stderr, ue.err)
		}
		return fail(stderr, err)
	}
	return 0
}

// usage 标记用法错误：Main 打印用法并返回 2。
type usageError struct{ err error }

func (u usageError) Error() string { return u.err.Error() }

func usagef(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

type runner struct {
	env  Env
	cfg  config.Config
	cap  *capture.Store
	sums *summary.Store
	mask *redact.Masker
}

func (r *runner) now() time.Time { return r.env.Now().In(time.Local) }

func (r *runner) today() string { return r.now().Format("2006-01-02") }

func (r *runner) maskText(s string) string { return r.mask.Mask(s) }

// dayList 返回采集日期和摘要日期的并集，最新的在前。
func (r *runner) dayList() ([]string, error) {
	captureDays, err := r.cap.Days()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(captureDays))
	for _, day := range append(captureDays, r.sums.Days()...) {
		if day == "" || seen[day] {
			continue
		}
		seen[day] = true
		out = append(out, day)
	}
	sortDaysDesc(out)
	return out, nil
}

func sortDaysDesc(days []string) {
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j] > days[j-1]; j-- {
			days[j], days[j-1] = days[j-1], days[j]
		}
	}
}

type options struct {
	values map[string]string
	flags  map[string]bool
	set    map[string]bool
	pos    []string
}

var valueFlags = map[string]bool{
	"limit": true, "date": true, "from": true, "to": true, "app": true, "days": true,
}

var boolFlags = map[string]bool{"ocr": true}

var commandFlags = map[string]map[string]bool{
	"status":    {},
	"days":      {"limit": true},
	"day":       {"date": true},
	"snapshots": {"date": true, "from": true, "to": true, "app": true, "ocr": true, "limit": true},
	"search":    {"days": true, "limit": true},
}

// parseArgs 把 flag 和位置参数分开。flag 可以写在位置参数前后。
func parseArgs(args []string) (string, options, error) {
	o := options{values: map[string]string{}, flags: map[string]bool{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			o.pos = append(o.pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			name := strings.TrimPrefix(a, "--")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				value := name[eq+1:]
				name = name[:eq]
				if !valueFlags[name] {
					return "", o, fmt.Errorf("unknown flag --%s", name)
				}
				o.values[name], o.set[name] = value, true
				continue
			}
			if valueFlags[name] {
				if i+1 >= len(args) {
					return "", o, fmt.Errorf("flag --%s needs a value", name)
				}
				i++
				o.values[name], o.set[name] = args[i], true
				continue
			}
			if boolFlags[name] {
				o.flags[name], o.set[name] = true, true
				continue
			}
			return "", o, fmt.Errorf("unknown flag --%s", name)
		}
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			return "", o, fmt.Errorf("unknown flag %s", a)
		}
		o.pos = append(o.pos, a)
	}
	if len(o.pos) == 0 {
		return "", o, nil
	}
	cmd := o.pos[0]
	if cmd != "search" && len(o.pos) > 1 {
		return "", o, fmt.Errorf("unexpected positional argument %q", o.pos[1])
	}
	allowed, ok := commandFlags[cmd]
	if !ok {
		return "", o, fmt.Errorf("unknown command %q", cmd)
	}
	for name := range o.set {
		if !allowed[name] {
			return "", o, fmt.Errorf("flag --%s is not valid for %s", name, cmd)
		}
	}
	return cmd, o, nil
}

func (r *runner) dateFlag(o options) (string, error) {
	if !o.set["date"] {
		return r.today(), nil
	}
	switch o.values["date"] {
	case "today":
		return r.today(), nil
	case "yesterday":
		return r.now().AddDate(0, 0, -1).Format("2006-01-02"), nil
	}
	day := o.values["date"]
	if _, err := time.ParseInLocation("2006-01-02", day, time.Local); err != nil {
		return "", usagef("invalid --date %q", day)
	}
	return day, nil
}

func intFlag(o options, name string, def, max int) (int, error) {
	if !o.set[name] {
		return def, nil
	}
	v, err := strconv.Atoi(o.values[name])
	if err != nil || v <= 0 {
		return 0, usagef("invalid --%s %q", name, o.values[name])
	}
	if max > 0 && v > max {
		v = max
	}
	return v, nil
}

func printUsageError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "konseki query: %s\n", err)
	fmt.Fprintln(stderr, usage)
	return 2
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "konseki query: %s\n", err)
	return 1
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func stamp(t time.Time) string { return t.In(time.Local).Format(time.RFC3339) }
