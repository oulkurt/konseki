/*
 * [INPUT]: 依赖事务元数据文件路径。
 * [OUTPUT]: 提供常量内存的事务 JSON 配置字段流式提取实现。
 * [POS]: 防止只读配置查询把事务中的密钥内容缓冲或解码到内存。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"unicode/utf16"
	"unicode/utf8"
)

type streamBase64Decoder struct {
	rem [4]byte
	n   int
	out bytes.Buffer
}

func (d *streamBase64Decoder) writeByte(c byte) error {
	if c == '\r' || c == '\n' {
		return nil
	}
	d.rem[d.n] = c
	d.n++
	if d.n < 4 {
		return nil
	}
	var dst [3]byte
	n, err := base64.StdEncoding.Decode(dst[:], d.rem[:])
	d.n = 0
	if err != nil {
		return err
	}
	d.out.Write(dst[:n])
	return nil
}

func (d *streamBase64Decoder) bytes() ([]byte, error) {
	if d.n != 0 {
		return nil, base64.CorruptInputError(d.out.Len())
	}
	b := d.out.Bytes()
	if b == nil {
		return []byte{}, nil
	}
	return b, nil
}

func readTxConfigOnly(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 4096)
	b, err := nextJSONByte(r)
	if err != nil {
		return nil, false, err
	}
	if b != '{' {
		return nil, false, errors.New("config: invalid tx json object")
	}
	b, err = nextJSONByte(r)
	if err != nil {
		return nil, false, err
	}
	if b == '}' {
		return nil, true, nil
	}
	for {
		if b != '"' {
			return nil, false, errors.New("config: invalid tx key")
		}
		isConfig, err := scanJSONKeyIs(r, "config")
		if err != nil {
			return nil, false, err
		}
		sep, err := nextJSONByte(r)
		if err != nil {
			return nil, false, err
		}
		if sep != ':' {
			return nil, false, errors.New("config: missing colon in tx json")
		}
		if isConfig {
			cfg, err := readJSONBase64Value(r)
			if err != nil {
				return nil, false, err
			}
			return cfg, true, nil
		}
		if err := skipJSONValue(r); err != nil {
			return nil, false, err
		}
		next, err := nextJSONByte(r)
		if err != nil {
			return nil, false, err
		}
		if next == '}' {
			return nil, true, nil
		}
		if next != ',' {
			return nil, false, errors.New("config: invalid tx delimiter")
		}
		b, err = nextJSONByte(r)
		if err != nil {
			return nil, false, err
		}
	}
}

func nextJSONByte(r *bufio.Reader) (byte, error) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return b, nil
		}
	}
}

func expectJSONLiteral(r *bufio.Reader, rest string) error {
	for i := 0; i < len(rest); i++ {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b != rest[i] {
			return errors.New("config: invalid json literal")
		}
	}
	return nil
}

func readHex4(r *bufio.Reader) (uint16, error) {
	var v uint16
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		var d uint16
		switch {
		case b >= '0' && b <= '9':
			d = uint16(b - '0')
		case b >= 'a' && b <= 'f':
			d = uint16(b-'a') + 10
		case b >= 'A' && b <= 'F':
			d = uint16(b-'A') + 10
		default:
			return 0, errors.New("config: invalid hex escape")
		}
		v = (v << 4) | d
	}
	return v, nil
}

func scanJSONStringBody(r *bufio.Reader, emit func(byte) error) error {
	var uBuf [utf8.UTFMax]byte
	for {
		b, err := r.ReadByte()
		if err != nil {
			return err
		}
		if b == '"' {
			return nil
		}
		if b < 0x20 {
			return errors.New("config: control byte in json string")
		}
		if b != '\\' {
			if emit != nil {
				if err := emit(b); err != nil {
					return err
				}
			}
			continue
		}
		esc, err := r.ReadByte()
		if err != nil {
			return err
		}
		var mapped byte
		hasByte := true
		switch esc {
		case '"', '\\', '/':
			mapped = esc
		case 'b':
			mapped = '\b'
		case 'f':
			mapped = '\f'
		case 'n':
			mapped = '\n'
		case 'r':
			mapped = '\r'
		case 't':
			mapped = '\t'
		case 'u':
			hasByte = false
			r1, err := readHex4(r)
			if err != nil {
				return err
			}
			var ru rune
			if r1 >= 0xD800 && r1 <= 0xDBFF {
				ru = utf8.RuneError
				if p, err := r.Peek(2); err == nil && p[0] == '\\' && p[1] == 'u' {
					_, _ = r.Discard(2)
					r2, err := readHex4(r)
					if err != nil {
						return err
					}
					if r2 >= 0xDC00 && r2 <= 0xDFFF {
						ru = utf16.DecodeRune(rune(r1), rune(r2))
					}
				}
			} else if r1 >= 0xDC00 && r1 <= 0xDFFF {
				ru = utf8.RuneError
			} else {
				ru = rune(r1)
			}
			if emit != nil {
				n := utf8.EncodeRune(uBuf[:], ru)
				for i := 0; i < n; i++ {
					if err := emit(uBuf[i]); err != nil {
						return err
					}
				}
			}
		default:
			return errors.New("config: invalid json escape")
		}
		if hasByte && emit != nil {
			if err := emit(mapped); err != nil {
				return err
			}
		}
	}
}

func scanJSONKeyIs(r *bufio.Reader, target string) (bool, error) {
	pos := 0
	matched := true
	err := scanJSONStringBody(r, func(b byte) error {
		if pos >= len(target) || b != target[pos] {
			matched = false
		}
		pos++
		return nil
	})
	if err != nil {
		return false, err
	}
	return matched && pos == len(target), nil
}

func readJSONBase64Value(r *bufio.Reader) ([]byte, error) {
	b, err := nextJSONByte(r)
	if err != nil {
		return nil, err
	}
	if b == 'n' {
		if err := expectJSONLiteral(r, "ull"); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if b != '"' {
		return nil, errors.New("config: expected base64 string for tx config")
	}
	var dec streamBase64Decoder
	if err := scanJSONStringBody(r, dec.writeByte); err != nil {
		return nil, err
	}
	return dec.bytes()
}

func skipJSONNumberTail(r *bufio.Reader) error {
	for {
		b, err := r.ReadByte()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if (b >= '0' && b <= '9') || b == '.' || b == 'e' || b == 'E' || b == '+' || b == '-' {
			continue
		}
		return r.UnreadByte()
	}
}

func skipJSONValue(r *bufio.Reader) error {
	b, err := nextJSONByte(r)
	if err != nil {
		return err
	}
	switch b {
	case '"':
		return scanJSONStringBody(r, nil)
	case 't':
		return expectJSONLiteral(r, "rue")
	case 'f':
		return expectJSONLiteral(r, "alse")
	case 'n':
		return expectJSONLiteral(r, "ull")
	case '{', '[':
		closer := byte('}')
		if b == '[' {
			closer = ']'
		}
		stack := []byte{closer}
		for len(stack) > 0 {
			c, err := nextJSONByte(r)
			if err != nil {
				return err
			}
			switch {
			case c == stack[len(stack)-1]:
				stack = stack[:len(stack)-1]
			case c == '{':
				stack = append(stack, '}')
			case c == '[':
				stack = append(stack, ']')
			case c == '"':
				if err := scanJSONStringBody(r, nil); err != nil {
					return err
				}
			case c == ':' || c == ',':
			case c == 't':
				if err := expectJSONLiteral(r, "rue"); err != nil {
					return err
				}
			case c == 'f':
				if err := expectJSONLiteral(r, "alse"); err != nil {
					return err
				}
			case c == 'n':
				if err := expectJSONLiteral(r, "ull"); err != nil {
					return err
				}
			case c == '-' || (c >= '0' && c <= '9'):
				if err := skipJSONNumberTail(r); err != nil {
					return err
				}
			default:
				return errors.New("config: invalid json token")
			}
		}
		return nil
	default:
		if b == '-' || (b >= '0' && b <= '9') {
			return skipJSONNumberTail(r)
		}
		return errors.New("config: invalid json value")
	}
}
