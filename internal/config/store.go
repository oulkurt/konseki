/*
 * [INPUT]: 依赖设置解析器和本地 JSON 文件。
 * [OUTPUT]: 提供 Store 的设置与密钥读写，以及只读的 Read。
 * [POS]: 配置模块的持久化边界，分离密钥与设置；Read 不建目录、不写文件、不读密钥。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Store 是设置文件的读写句柄。
type Store struct {
	dir string
	mu  sync.Mutex
	cfg Config
}

// secretsFile 是密钥的落盘位置，和主配置分开。
const secretsFile = "secrets.json"

// Open 打开（或创建）数据目录里的设置。
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, cfg: Default()}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Read 只读设置：不建目录、不写文件、不读 secrets.json。
// 文件不存在时返回默认值，供只读命令和诊断使用。
func Read(dir string) (Config, error) {
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg, _, err := parse(b)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (s *Store) path() string       { return filepath.Join(s.dir, "config.json") }
func (s *Store) secretPath() string { return filepath.Join(s.dir, secretsFile) }

// load 读主配置和密钥。文件不存在不算错 —— 第一次启动本来就没有。
func (s *Store) load() error {
	b, err := os.ReadFile(s.path())
	switch {
	case err == nil:
		cfg, legacy, err := parse(b)
		if err != nil {
			return err
		}
		s.cfg = cfg
		if legacy {
			backup := filepath.Join(s.dir, "config.v1.json")
			f, e := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if e == nil {
				if _, e = f.Write(b); e != nil {
					f.Close()
					return e
				}
				if e = f.Close(); e != nil {
					return e
				}
			} else if !errors.Is(e, os.ErrExist) {
				return e
			}
			if err := s.loadSecrets(); err != nil {
				return err
			}
			return s.saveLocked()
		}
	case errors.Is(err, os.ErrNotExist):
		s.cfg = Default()
	default:
		return err
	}
	return s.loadSecrets()
}

type secretEntry struct {
	APIKey       string `json:"apiKey,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

func (s *Store) loadSecrets() error {
	b, err := os.ReadFile(s.secretPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var m map[string]secretEntry
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	for i := range s.cfg.Providers {
		p := &s.cfg.Providers[i]
		if e, ok := m[p.Preset+"|"+p.Label]; ok {
			p.APIKey = e.APIKey
			p.AccessToken = e.AccessToken
			p.RefreshToken = e.RefreshToken
		}
	}
	return nil
}

// writeSecrets 把密钥单独写一份 0600 的文件。
func (s *Store) writeSecrets() error {
	m := map[string]secretEntry{}
	for _, p := range s.cfg.Providers {
		if p.APIKey == "" && p.AccessToken == "" && p.RefreshToken == "" {
			continue
		}
		m[p.Preset+"|"+p.Label] = secretEntry{
			APIKey: p.APIKey, AccessToken: p.AccessToken, RefreshToken: p.RefreshToken,
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	// 先写临时文件再 rename：中途崩溃不会留下半截的密钥文件。
	tmp := s.secretPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.secretPath())
}

// Get 返回当前设置的副本。
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.clone()
}

// Update 用 fn 改设置并落盘。fn 拿到的是副本，改动成功后才会生效 ——
// 这样 fn 里 panic 或返回逻辑错误时，内存和磁盘不会不一致。
func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg.clone()
	fn(&next)
	normalize(&next)
	s.cfg = next
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		return err
	}
	return s.writeSecrets()
}

// clone 深拷贝，并保留 nil 与空切片的区别：空切片落盘是 []，表示用户清空了列表。
func (c Config) clone() Config {
	out := c
	out.Recorder.SensitiveApps = slices.Clone(c.Recorder.SensitiveApps)
	out.Recorder.OCR.Languages = slices.Clone(c.Recorder.OCR.Languages)
	out.Providers = slices.Clone(c.Providers)
	for i := range out.Providers {
		out.Providers[i].EnabledModels = slices.Clone(out.Providers[i].EnabledModels)
	}
	out.Redact.Kinds = maps.Clone(c.Redact.Kinds)
	out.Redact.Allow = slices.Clone(c.Redact.Allow)
	out.Redact.Words = slices.Clone(c.Redact.Words)
	return out
}
