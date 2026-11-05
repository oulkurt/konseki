/**
 * [INPUT]: 依赖 Unix 假服务端和临时隔离目录。
 * [OUTPUT]: 验证协议编码、回复匹配、错误和启动配置。
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
)

func TestClient(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "live")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
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
		var req struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]int `json:"params"`
		}
		if err = dec.Decode(&req); err != nil {
			done <- err
			return
		}
		if req.ID != 1 || req.Method != "capture" || req.Params["window"] != 42 {
			done <- errors.New("请求编码错误")
			return
		}
		enc.Encode(map[string]any{"id": 999, "result": map[string]any{"png": []byte("wrong")}})
		enc.Encode(map[string]any{"id": 1, "result": map[string]any{"png": []byte("image")}})
		if err = dec.Decode(&req); err != nil {
			done <- err
			return
		}
		err = enc.Encode(map[string]any{"id": req.ID, "error": map[string]string{"code": "no_window", "message": "missing"}})
		done <- err
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
	err = c.call("idle", nil, nil)
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
