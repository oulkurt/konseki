/**
 * [INPUT]: 依赖 Unix 假服务端和临时隔离目录。
 * [OUTPUT]: 验证协议编码、回复匹配、错误、等待上限、启动配置、套接字占用和 CPU 时间解析。
 * [POS]: live 传输与配置的回归测试，不启动图形程序。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]int `json:"params"`
}

// shortSocket 返回一个短路径。macOS 的 Unix 套接字路径不能超过 104 字节，t.TempDir() 太长。
func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "live")
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
	}
	if err = json.Unmarshal(b, &cfg); err != nil || cfg.Version != 2 || cfg.Language != "en" || !cfg.Recorder.Enabled {
		t.Fatalf("配置 %s: %v", b, err)
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
	if err := launch("/bin/true", home, path, "zh-Hans", false); err == nil {
		t.Fatal("套接字被占用时仍然启动")
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("拒绝启动前已经写了 home")
	}
	l.Close()
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
