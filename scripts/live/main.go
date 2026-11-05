/**
 * [INPUT]: 依赖自动化客户端、隔离目录和系统进程采样。
 * [OUTPUT]: 提供窗口驱动和性能采样命令。
 * [POS]: live 命令入口，启动配置与协议传输分离。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

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
	return os.WriteFile(filepath.Join(home, "config.json"), b, 0600)
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("用法: live launch|shot|click|wait|perf [参数]")
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
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "launch":
		if *bin == "" || *sock == "" {
			return errors.New("必须指定 bin 和 sock")
		}
		if err := prepareHome(*home, *lang, *rec); err != nil {
			return err
		}
		cmd := exec.Command(*bin)
		cmd.Env = append(os.Environ(), "KONSEKI_HOME="+*home, "MYGO_AUTOMATION="+*sock)
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
				c, err := net.Dial("unix", *sock)
				if err == nil {
					c.Close()
					fmt.Println(cmd.Process.Pid)
					return nil
				}
			}
		}
	case "perf":
		if *pid <= 0 || *secs <= 0 {
			return errors.New("必须指定正数 pid 和 secs")
		}
		var sumCPU, maxCPU, sumRSS, maxRSS float64
		for i := 0; i < *secs; i++ {
			b, err := exec.Command("ps", "-o", "%cpu=,rss=", "-p", fmt.Sprint(*pid)).Output()
			if err != nil {
				return err
			}
			var cpu, rss float64
			if _, err = fmt.Sscan(string(b), &cpu, &rss); err != nil {
				return err
			}
			rss /= 1024
			sumCPU += cpu
			sumRSS += rss
			if cpu > maxCPU {
				maxCPU = cpu
			}
			if rss > maxRSS {
				maxRSS = rss
			}
			if i+1 < *secs {
				time.Sleep(time.Second)
			}
		}
		fmt.Printf("平均 CPU %.2f%% 峰值 CPU %.2f%% 平均 RSS %.2f MB 峰值 RSS %.2f MB\n", sumCPU/float64(*secs), maxCPU, sumRSS/float64(*secs), maxRSS)
		return nil
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
		c, err := dial(*sock)
		if err != nil {
			return err
		}
		defer c.conn.Close()
		switch args[0] {
		case "wait":
			return c.call("idle", nil, nil)
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
			if err := os.MkdirAll(*out, 0755); err != nil {
				return err
			}
			for _, w := range result.Windows {
				b, err := c.capture(w.ID)
				if err != nil {
					return err
				}
				if err = os.WriteFile(filepath.Join(*out, fmt.Sprintf("window-%d.png", w.ID)), b, 0644); err != nil {
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
