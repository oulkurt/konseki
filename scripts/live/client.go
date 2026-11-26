/**
 * [INPUT]: 依赖 MyGo 自动化 Unix 套接字协议。
 * [OUTPUT]: 提供串行化请求、协议错误、截止时间保护、带上限的等待、窗口列表和截图解码。
 * [POS]: live 命令的传输边界，隔离窗口操作与协议编码。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	defaultRPCTimeout = 15 * time.Second
	idleDeadlineSlack = 5 * time.Second
)

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *protocolError) Error() string { return e.Code + ": " + e.Message }

type client struct {
	mu       sync.Mutex
	conn     net.Conn
	enc      *json.Encoder
	dec      *json.Decoder
	next     int
	poisoned bool
}

func dial(path string) (*client, error) {
	c, e := net.Dial("unix", path)
	if e != nil {
		return nil, e
	}
	return &client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}, nil
}

func (c *client) call(method string, params any, out any) error {
	return c.callTimeout(method, params, out, defaultRPCTimeout)
}

func (c *client) callTimeout(method string, params any, out any, wait time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.poisoned {
		return errors.New("客户端连接已失效")
	}
	if err := c.conn.SetDeadline(time.Now().Add(wait)); err != nil {
		c.poisoned = true
		return err
	}
	defer func() { _ = c.conn.SetDeadline(time.Time{}) }()
	c.next++
	id := c.next
	if err := c.enc.Encode(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params}); err != nil {
		c.poisoned = true
		return err
	}
	for {
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *protocolError  `json:"error"`
		}
		if err := c.dec.Decode(&resp); err != nil {
			c.poisoned = true
			return err
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil {
			trimmed := bytes.TrimSpace(resp.Result)
			if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
				return &protocolError{Code: "empty_result", Message: fmt.Sprintf("%s 返回空结果", method)}
			}
			return json.Unmarshal(trimmed, out)
		}
		return nil
	}
}

// idle 等焦点窗口画完。插件把 timeout 为 0 当作不设上限，
// 动画不停的窗口会让命令永远等下去，所以总是带上限。
func (c *client) idle(timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("timeout 必须为正数")
	}
	ms := max(int64(1), timeout.Milliseconds())
	wait := timeout + idleDeadlineSlack
	if wait < timeout {
		wait = timeout
	}
	return c.callTimeout("idle", map[string]any{"timeout": ms}, nil, wait)
}

func (c *client) windows() ([]int, error) {
	var result struct {
		Windows []struct {
			ID int `json:"id"`
		} `json:"windows"`
	}
	if err := c.call("windows", nil, &result); err != nil {
		return nil, err
	}
	if len(result.Windows) == 0 {
		return nil, errors.New("没有可用窗口")
	}
	ids := make([]int, len(result.Windows))
	for i, w := range result.Windows {
		ids[i] = w.ID
	}
	return ids, nil
}

func (c *client) capture(id int) ([]byte, error) {
	var r struct {
		PNG []byte `json:"png"`
	}
	err := c.call("capture", map[string]any{"window": id}, &r)
	if err != nil {
		return nil, err
	}
	if len(r.PNG) == 0 {
		return nil, fmt.Errorf("空截图")
	}
	return r.PNG, nil
}
