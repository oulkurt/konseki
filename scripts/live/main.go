/**
 * [INPUT]: 依赖自动化客户端、隔离目录和 ps 的进程采样。
 * [OUTPUT]: 提供 launch、shot、click、wait、perf 五个命令。
 * [POS]: live 命令入口。启动隔离和进程采样在这里，协议编码在 client.go。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const usage = "用法: live launch|shot|click|wait|perf [参数]"

func prepareHome(home, lang string, recorder bool) error {
	if home == "" {
		return errors.New("缺少 home")
	}
	if lang != "zh-Hans" && lang != "en" {
		return errors.New("无效语言")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("home 必须为空目录")
	}
	b, err := json.Marshal(map[string]any{"version": 2, "language": lang, "recorder": map[string]bool{"enabled": recorder}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, "config.json"), b, 0o600)
}

// socketBusy 报告 path 上是否已有程序在监听。插件不会抢别人正在用的套接字，
// 那时新进程不监听，等待循环却会连上旧进程，后续命令全打到错的 App 上。
func socketBusy(path string) bool {
	c, err := net.Dial("unix", path)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func launch(bin, home, sock, lang string, recorder bool) error {
	if bin == "" || sock == "" {
		return errors.New("必须指定 bin 和 sock")
	}
	if socketBusy(sock) {
		return fmt.Errorf("%s 上已有程序在监听，换一个套接字路径", sock)
	}
	if err := prepareHome(home, lang, recorder); err != nil {
		return err
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "KONSEKI_HOME="+home, "MYGO_AUTOMATION="+sock)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return fmt.Errorf("进程提前退出: %v", err)
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return errors.New("等待套接字超时")
		case <-ticker.C:
			if socketBusy(sock) {
				fmt.Println(cmd.Process.Pid)
				return nil
			}
		}
	}
}

type psSample struct {
	at      time.Time
	cpuTime time.Duration
	cpu     float64 // macOS 的 %cpu 是约一分钟的衰减平均，只拿来看峰值
	rssMB   float64
}

func samplePS(pid int) (psSample, error) {
	cmd := exec.Command("ps", "-o", "time=,%cpu=,rss=", "-p", strconv.Itoa(pid))
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	b, err := cmd.Output()
	if err != nil {
		return psSample{}, fmt.Errorf("读取进程 %d: %w", pid, err)
	}
	f := strings.Fields(string(b))
	if len(f) != 3 {
		return psSample{}, fmt.Errorf("无法解析 ps 输出 %q", b)
	}
	cpuTime, err := parseCPUTime(f[0])
	if err != nil {
		return psSample{}, err
	}
	cpu, err := strconv.ParseFloat(f[1], 64)
	if err != nil {
		return psSample{}, err
	}
	rss, err := strconv.ParseFloat(f[2], 64)
	if err != nil {
		return psSample{}, err
	}
	return psSample{at: time.Now(), cpuTime: cpuTime, cpu: cpu, rssMB: rss / 1024}, nil
}

// parseCPUTime 解析 ps 的累计 CPU 时间。macOS 写成「分:秒.百分秒」，
// 超过一小时也不进位（614:06.90），也接受「时:分:秒」和「天-」前缀。
func parseCPUTime(s string) (time.Duration, error) {
	rest, days := s, 0
	if d, r, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, fmt.Errorf("无法解析 CPU 时间 %q", s)
		}
		rest, days = r, n
	}
	secs := 0.0
	for _, part := range strings.Split(rest, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, fmt.Errorf("无法解析 CPU 时间 %q", s)
		}
		secs = secs*60 + v
	}
	return time.Duration(days)*24*time.Hour + time.Duration(math.Round(secs*float64(time.Second))), nil
}

// perf 的平均 CPU 取累计 CPU 时间的增量除以墙钟时间。逐秒平均 %cpu
// 会把采样开始前一分钟的占用也带进来，App 刚启动时尤其偏高。
func perf(pid, secs int) error {
	first, err := samplePS(pid)
	if err != nil {
		return err
	}
	last := first
	var peakCPU, sumRSS, peakRSS float64
	for range secs {
		time.Sleep(time.Second)
		if last, err = samplePS(pid); err != nil {
			return err
		}
		peakCPU = max(peakCPU, last.cpu)
		sumRSS += last.rssMB
		peakRSS = max(peakRSS, last.rssMB)
	}
	avg := (last.cpuTime - first.cpuTime).Seconds() / last.at.Sub(first.at).Seconds() * 100
	fmt.Printf("平均 CPU %.2f%% 峰值 CPU %.2f%% 平均 RSS %.2f MB 峰值 RSS %.2f MB\n", avg, peakCPU, sumRSS/float64(secs), peakRSS)
	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	sock := f.String("sock", "", "套接字路径")
	bin := f.String("bin", "", "二进制路径")
	home := f.String("home", "", "空目录")
	lang := f.String("lang", "zh-Hans", "语言")
	rec := f.Bool("recorder", false, "启用记录器")
	out := f.String("out", "", "输出目录")
	pid := f.Int("pid", 0, "进程号")
	secs := f.Int("secs", 0, "采样秒数")
	timeout := f.Duration("timeout", 10*time.Second, "wait 的上限")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "launch":
		return launch(*bin, *home, *sock, *lang, *rec)
	case "perf":
		if *pid <= 0 || *secs <= 0 {
			return errors.New("必须指定正数 pid 和 secs")
		}
		return perf(*pid, *secs)
	case "shot", "click", "wait":
		if *sock == "" {
			return errors.New("必须指定 sock")
		}
		if args[0] == "shot" && *out == "" {
			return errors.New("必须指定 out")
		}
		if args[0] == "click" && f.NArg() != 1 {
			return errors.New("必须指定一个 key")
		}
		if args[0] == "wait" && *timeout <= 0 {
			return errors.New("timeout 必须为正数")
		}
		c, err := dial(*sock)
		if err != nil {
			return err
		}
		defer c.conn.Close()
		switch args[0] {
		case "wait":
			return c.idle(*timeout)
		case "click":
			return c.call("act", map[string]any{"key": f.Arg(0), "action": "press"}, nil)
		case "shot":
			var result struct {
				Windows []struct {
					ID int `json:"id"`
				} `json:"windows"`
			}
			if err := c.call("windows", nil, &result); err != nil {
				return err
			}
			if err := os.MkdirAll(*out, 0o755); err != nil {
				return err
			}
			for _, w := range result.Windows {
				b, err := c.capture(w.ID)
				if err != nil {
					return err
				}
				if err = os.WriteFile(filepath.Join(*out, fmt.Sprintf("window-%d.png", w.ID)), b, 0o644); err != nil {
					return err
				}
			}
			return nil
		}
	}
	return fmt.Errorf("未知命令: %s", strings.Join(args, " "))
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
