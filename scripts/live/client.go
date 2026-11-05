/**
 * [INPUT]: 依赖 MyGo 自动化 Unix 套接字协议。
 * [OUTPUT]: 提供请求、协议错误和截图解码。
 * [POS]: live 命令的传输边界，隔离窗口操作与协议编码。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"encoding/json"
	"fmt"
	"net"
)

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *protocolError) Error() string { return e.Code + ": " + e.Message }

type client struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder
	next int
}

func dial(path string) (*client, error) {
	c, e := net.Dial("unix", path)
	if e != nil {
		return nil, e
	}
	return &client{conn: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}, nil
}
func (c *client) call(method string, params any, out any) error {
	c.next++
	id := c.next
	if err := c.enc.Encode(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params}); err != nil {
		return err
	}
	for {
		var resp struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *protocolError  `json:"error"`
		}
		if err := c.dec.Decode(&resp); err != nil {
			return err
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return resp.Error
		}
		if out != nil {
			return json.Unmarshal(resp.Result, out)
		}
		return nil
	}
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
