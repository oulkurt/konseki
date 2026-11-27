/**
 * [INPUT]: 依赖 Unix 假服务端和临时隔离目录。
 * [OUTPUT]: 验证协议编码、回复匹配、错误和等待上限。
 * 验证启动配置、子进程许可、套接字占用、种子摘要和 CPU 时间解析。
 * [POS]: live 传输与配置的回归测试，不启动图形程序。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"konseki/internal/summary"
)

func TestMain(m *testing.M) {
	switch os.Getenv("KONSEKI_TEST_LIVE_HELPER") {
	case "early":
		_, _ = fmt.Fprintln(os.Stderr, strings.Repeat("boom-", 2500))
		os.Exit(1)
	case "hang":
		if pidFile := os.Getenv("KONSEKI_TEST_LIVE_PIDFILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "stderr_child":
		sock := os.Getenv("MYGO_AUTOMATION")
		l, err := net.Listen("unix", sock)
		if err != nil {
			os.Exit(2)
		}
		defer l.Close()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			var req struct {
				ID     int    `json:"id"`
				Method string `json:"method"`
			}
			_ = json.NewDecoder(conn).Decode(&req)
			switch req.Method {
			case "check_stderr":
				_, _ = fmt.Fprintln(os.Stderr, "after-launch-stderr-log")
				_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": map[string]any{"alive": true}})
			case "quit":
				_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": map[string]any{"bye": true}})
				_ = conn.Close()
				os.Exit(0)
			}
			_ = conn.Close()
		}
	default:
		os.Exit(m.Run())
	}
}

type fakeRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]int `json:"params"`
}

// shortSocket 返回一个短路径。macOS 的 Unix 套接字路径不能超过 104 字节，t.TempDir() 太长。
func shortSocket(t *testing.T) string {
	t.Helper()
	tmpRoot := "/tmp"
	if runtime.GOOS == "windows" {
		tmpRoot = ""
	}
	dir, err := os.MkdirTemp(tmpRoot, "live")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func TestClient(t *testing.T) {
	path := shortSocket(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		var capture fakeRequest
		if err = dec.Decode(&capture); err != nil {
			done <- err
			return
		}
		if capture.ID != 1 || capture.Method != "capture" || capture.Params["window"] != 42 {
			done <- errors.New("capture 请求编码错误")
			return
		}
		enc.Encode(map[string]any{"id": 999, "result": map[string]any{"png": []byte("wrong")}})
		enc.Encode(map[string]any{"id": 1, "result": map[string]any{"png": []byte("image")}})
		var idle fakeRequest
		if err = dec.Decode(&idle); err != nil {
			done <- err
			return
		}
		if idle.Method != "idle" || idle.Params["timeout"] != 1500 {
			done <- errors.New("idle 请求没有带上限")
			return
		}
		done <- enc.Encode(map[string]any{"id": idle.ID, "error": map[string]string{"code": "no_window", "message": "missing"}})
	}()
	c, err := dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.conn.Close()
	b, err := c.capture(42)
	if err != nil || !bytes.Equal(b, []byte("image")) {
		t.Fatalf("截图 %q: %v", b, err)
	}
	err = c.idle(1500 * time.Millisecond)
	var pe *protocolError
	if !errors.As(err, &pe) || pe.Code != "no_window" {
		t.Fatalf("错误: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPrepareHome(t *testing.T) {
	dir := t.TempDir()
	if err := prepareHome(dir, "en", true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Version  int    `json:"version"`
		Language string `json:"language"`
		Recorder struct {
			Enabled bool `json:"enabled"`
		} `json:"recorder"`
		Skill struct {
			ExportDir string `json:"exportDir"`
		} `json:"skill"`
	}
	if err = json.Unmarshal(b, &cfg); err != nil || cfg.Version != 2 || cfg.Language != "en" || !cfg.Recorder.Enabled {
		t.Fatalf("配置 %s: %v", b, err)
	}
	// 清除会对账技能目录，它必须在隔离目录里，不能落到真实 home。
	if rel, err := filepath.Rel(dir, cfg.Skill.ExportDir); err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("技能目录 %q 不在 %q 里", cfg.Skill.ExportDir, dir)
	}
	if err := prepareHome(dir, "en", false); err == nil {
		t.Fatal("非空目录被接受")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if !bytes.Equal(b, after) {
		t.Fatal("配置被覆盖")
	}
}

func TestSocketBusy(t *testing.T) {
	path := shortSocket(t)
	if socketBusy(path) {
		t.Fatal("没有监听时报告占用")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if !socketBusy(path) {
		t.Fatal("有程序监听时没有报告占用")
	}
	home := t.TempDir()
	if err := launch("/bin/true", home, path, "zh-Hans", false, true); err == nil {
		t.Fatal("套接字被占用时仍然启动")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("拒绝启动前已经写了 home")
	}
	l.Close()
}

// 种子数据是今天的三段摘要，用 App 自己的摘要存储读得回来。
func TestSeedHomeWritesTodaySummaries(t *testing.T) {
	home := t.TempDir()
	if err := seedHome(home); err != nil {
		t.Fatal(err)
	}
	st, err := summary.NewStore(home)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	got, err := st.Load(today)
	if err != nil || len(got) != 3 {
		t.Fatalf("今天的摘要 %d 段，错误 %v", len(got), err)
	}
	for _, e := range got {
		if e.Day != today || e.Start.Format("2006-01-02") != today {
			t.Fatalf("摘要不在今天：%+v", e)
		}
	}
}

func TestParseCPUTime(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"0:00.36", 360 * time.Millisecond},
		{"614:06.90", 614*time.Minute + 6900*time.Millisecond},
		{"1:02:03.25", time.Hour + 2*time.Minute + 3250*time.Millisecond},
		{"2-01:00:00", 49 * time.Hour},
	}
	for _, c := range cases {
		got, err := parseCPUTime(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseCPUTime(%q) = %v, %v，期望 %v", c.in, got, err, c.want)
		}
	}
	if _, err := parseCPUTime("1:xx"); err == nil {
		t.Error("坏输入没有报错")
	}
}

func TestLaunchChildEnvironment(t *testing.T) {
	if os.Getenv("KONSEKI_TEST_LAUNCH_ENV") == "1" {
		out := map[string]string{}
		for _, key := range []string{"KONSEKI_HOME", "MYGO_AUTOMATION", "KONSEKI_E2E_ALLOW_CAPTURE"} {
			out[key] = os.Getenv(key)
		}
		_ = json.NewEncoder(os.Stdout).Encode(out)
		os.Exit(0)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, inherited := range []string{"", "0", "1", "true"} {
		for _, recorder := range []bool{false, true} {
			t.Setenv("KONSEKI_E2E_ALLOW_CAPTURE", inherited)
			t.Setenv("KONSEKI_HOME", "inherited-home")
			t.Setenv("MYGO_AUTOMATION", "inherited-socket")
			cmd := launchCommand(bin, "fake-home", "fake-socket", recorder)
			count := 0
			for _, entry := range cmd.Env {
				if strings.HasPrefix(entry, "KONSEKI_E2E_ALLOW_CAPTURE=") {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("许可变量数量=%d", count)
			}
			cmd.Args = append(cmd.Args, "-test.run=^TestLaunchChildEnvironment$")
			cmd.Env = append(cmd.Env, "KONSEKI_TEST_LAUNCH_ENV=1")
			b, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]string
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			want := "0"
			if recorder {
				want = "1"
			}
			if got["KONSEKI_E2E_ALLOW_CAPTURE"] != want || got["KONSEKI_HOME"] != "fake-home" || got["MYGO_AUTOMATION"] != "fake-socket" {
				t.Fatalf("继承=%q recorder=%v: %v", inherited, recorder, got)
			}
		}
	}
}

func TestClientSubMillisecondIdleAndValidation(t *testing.T) {
	path := shortSocket(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	gotTimeout := make(chan int, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req fakeRequest
		if err := json.NewDecoder(conn).Decode(&req); err != nil {
			return
		}
		gotTimeout <- req.Params["timeout"]
		_ = json.NewEncoder(conn).Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
	}()

	c, err := dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.conn.Close()

	if err := c.idle(0); err == nil {
		t.Fatal("idle(0) succeeded")
	}
	if err := c.idle(-time.Millisecond); err == nil {
		t.Fatal("idle(-1ms) succeeded")
	}
	if err := c.idle(500 * time.Microsecond); err != nil {
		t.Fatalf("idle(500us) failed: %v", err)
	}
	if ms := <-gotTimeout; ms != 1 {
		t.Fatalf("sub-millisecond timeout = %d, want 1", ms)
	}
}

func TestClientEmptyResultPoisonAndConcurrency(t *testing.T) {
	path := shortSocket(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)
		for range 5 {
			var req fakeRequest
			if err := dec.Decode(&req); err != nil {
				return
			}
			switch req.Method {
			case "null_result":
				_ = enc.Encode(map[string]any{"id": req.ID, "result": nil})
			case "missing_result":
				_ = enc.Encode(map[string]any{"id": req.ID})
			case "ok":
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]int{"n": req.ID}})
			}
		}
	}()

	c, err := dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.conn.Close()

	for _, method := range []string{"null_result", "missing_result"} {
		var out map[string]any
		err := c.call(method, nil, &out)
		var pe *protocolError
		if !errors.As(err, &pe) || pe.Code != "empty_result" {
			t.Fatalf("%s err = %v, want empty_result protocolError", method, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var res struct {
				N int `json:"n"`
			}
			errs <- c.call("ok", nil, &res)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent call failed: %v", err)
		}
	}
}

func TestClientPoisonedAfterDeadline(t *testing.T) {
	path := shortSocket(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	release := make(chan struct{})
	defer close(release)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req fakeRequest
		_ = json.NewDecoder(conn).Decode(&req)
		<-release
	}()

	c, err := dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.conn.Close()

	var out map[string]any
	if err := c.callTimeout("hang", nil, &out, 40*time.Millisecond); err == nil {
		t.Fatal("expected deadline error")
	}
	start := time.Now()
	err = c.callTimeout("after", nil, &out, time.Second)
	if err == nil || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("poisoned client did not fail fast: err=%v elapsed=%v", err, time.Since(start))
	}
}

func TestActGuardAndEmptyWindows(t *testing.T) {
	path := shortSocket(t)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				dec := json.NewDecoder(conn)
				enc := json.NewEncoder(conn)
				for {
					var req struct {
						ID     int            `json:"id"`
						Method string         `json:"method"`
						Params map[string]any `json:"params"`
					}
					if err := dec.Decode(&req); err != nil {
						return
					}
					switch req.Method {
					case "windows":
						_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{"windows": []any{}}})
					case "act":
						_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
					}
				}
			}(conn)
		}
	}()

	for _, args := range [][]string{
		{"act", "-sock", path},
		{"act", "-sock", path, "-id", "n1", "extra"},
		{"act", "-sock", path, "-id", "n1", "extra1", "extra2"},
		{"act", "-sock", path, "k1", "k2"},
	} {
		if err := run(args); err == nil {
			t.Fatalf("run(%v) succeeded, want error", args)
		}
	}
	if err := run([]string{"act", "-sock", path, "-id", "n1"}); err != nil {
		t.Fatalf("act -id failed: %v", err)
	}
	if err := run([]string{"act", "-sock", path, "btn"}); err != nil {
		t.Fatalf("act key failed: %v", err)
	}

	if err := run([]string{"tree", "-sock", path}); err == nil {
		t.Fatal("tree with empty windows succeeded")
	}
	shotOut := filepath.Join(t.TempDir(), "shots")
	if err := run([]string{"shot", "-sock", path, "-out", shotOut}); err == nil {
		t.Fatal("shot with empty windows succeeded")
	}
	if _, err := os.Stat(shotOut); !os.IsNotExist(err) {
		t.Fatalf("shot created output dir despite empty windows: %v", err)
	}
}

func TestHomeNormalizationAndLaunchRollback(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	relHome := "rel-home"
	if err := os.MkdirAll(relHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"seed", "-home", relHome}); err != nil {
		t.Fatalf("seed relative home failed: %v", err)
	}
	absHome, err := filepath.Abs(relHome)
	if err != nil {
		t.Fatal(err)
	}
	st, err := summary.NewStore(absHome)
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := st.Load(time.Now().Format("2006-01-02")); err != nil || len(entries) != 3 {
		t.Fatalf("seeded entries = %d, err = %v", len(entries), err)
	}

	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("early", func(t *testing.T) {
		earlyHome := filepath.Join(base, "early-home")
		if err := os.MkdirAll(earlyHome, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KONSEKI_TEST_LIVE_HELPER", "early")
		err := launchWithTimeout(bin, earlyHome, shortSocket(t), "zh-Hans", false, true, 5*time.Second)
		if err == nil || !strings.Contains(err.Error(), "boom-") || !strings.Contains(err.Error(), "...(truncated)") {
			t.Fatalf("early exit error = %v", err)
		}
		if entries, err := os.ReadDir(earlyHome); err != nil || len(entries) != 0 {
			t.Fatalf("earlyHome not rolled back: %v, %v", entries, err)
		}
	})

	t.Run("hang", func(t *testing.T) {
		pidFile := filepath.Join(base, "hang.pid")
		hangHome := filepath.Join(base, "hang-home")
		if err := os.MkdirAll(hangHome, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("KONSEKI_TEST_LIVE_HELPER", "hang")
		t.Setenv("KONSEKI_TEST_LIVE_PIDFILE", pidFile)
		err := launchWithTimeout(bin, hangHome, shortSocket(t), "en", false, true, 500*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "超时") {
			t.Fatalf("timeout error = %v", err)
		}
		if entries, err := os.ReadDir(hangHome); err != nil || len(entries) != 0 {
			t.Fatalf("hangHome not rolled back: %v, %v", entries, err)
		}
		rawPID, readErr := os.ReadFile(pidFile)
		if readErr != nil {
			t.Fatalf("hang helper did not record pid: %v", readErr)
		}
		var childPID int
		if _, scanErr := fmt.Sscan(string(rawPID), &childPID); scanErr != nil || childPID <= 0 {
			t.Fatalf("invalid hang pid %q: %v", rawPID, scanErr)
		}
		if alive, sigErr := processAlive(childPID); sigErr != nil || alive {
			t.Fatalf("timed-out child %d still alive/unreaped: alive=%v err=%v", childPID, alive, sigErr)
		}
	})
}

func TestLaunchChildStderrAfterLauncherExit(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	home := filepath.Join(base, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	sock := shortSocket(t)
	t.Setenv("KONSEKI_TEST_LIVE_HELPER", "stderr_child")
	if err := launchWithTimeout(bin, home, sock, "zh-Hans", false, false, 5*time.Second); err != nil {
		t.Fatalf("launchWithTimeout failed: %v", err)
	}
	t.Cleanup(func() {
		if q, err := dial(sock); err == nil {
			var quitRes map[string]any
			_ = q.call("quit", nil, &quitRes)
			_ = q.conn.Close()
		}
	})

	c, err := dial(sock)
	if err != nil {
		t.Fatalf("dial launched child: %v", err)
	}
	var res struct {
		Alive bool `json:"alive"`
	}
	if err := c.call("check_stderr", nil, &res); err != nil || !res.Alive {
		_ = c.conn.Close()
		t.Fatalf("child exited on post-launch stderr write: res=%+v err=%v", res, err)
	}
	_ = c.conn.Close()

	c2, err := dial(sock)
	if err == nil {
		var quitRes map[string]any
		_ = c2.call("quit", nil, &quitRes)
		_ = c2.conn.Close()
	}

	raw, err := os.ReadFile(filepath.Join(home, "stderr.log"))
	if err != nil || !strings.Contains(string(raw), "after-launch-stderr-log") {
		t.Fatalf("stderr.log missing child output: %q err=%v", raw, err)
	}
}
