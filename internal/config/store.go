/*
 * [INPUT]: 依赖设置解析器和本地 JSON 文件。
 * [OUTPUT]: 提供 Store 的设置与密钥读写，以及只读的 Read。
 * [POS]: 配置模块的持久化边界，分离密钥与设置；Read 不建目录、不写文件、不读密钥。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Store 是设置文件的读写句柄。
type Store struct {
	dir              string
	mu               sync.Mutex
	cfg              Config
	preservedSecrets map[string]secretEntry
}

// secretsFile 是密钥的落盘位置，和主配置分开。
const secretsFile = "secrets.json"

const txFile = ".config.tx.json"

const secretsTxFile = ".secrets.tx"

const unclaimedSecretPrefix = "unclaimed|"

type configTx struct {
	Config        []byte `json:"config"`
	Secrets       []byte `json:"secrets,omitempty"`
	SecretsSHA256 string `json:"secretsSha256,omitempty"`
}

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
	diskB, diskOK, err := readIfExists(filepath.Join(dir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	txCfg, txOK, err := readTxConfigOnly(filepath.Join(dir, txFile))
	if err != nil {
		return Config{}, err
	}
	if txOK && len(txCfg) > 0 {
		useTx := true
		if diskOK {
			diskVer, dErr := configBytesVersion(diskB)
			txVer, tErr := configBytesVersion(txCfg)
			if dErr == nil && tErr == nil && diskVer > CurrentVersion && txVer < diskVer {
				useTx = false
			}
		}
		if useTx {
			cfg, _, err := parse(txCfg)
			if err != nil {
				return Config{}, err
			}
			return cfg, nil
		}
	}
	if !diskOK {
		return Default(), nil
	}
	cfg, _, err := parse(diskB)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (s *Store) path() string         { return filepath.Join(s.dir, "config.json") }
func (s *Store) secretPath() string   { return filepath.Join(s.dir, secretsFile) }
func (s *Store) txPath() string       { return filepath.Join(s.dir, txFile) }
func (s *Store) secretTxPath() string { return filepath.Join(s.dir, secretsTxFile) }

// load 读主配置和密钥。文件不存在不算错 —— 第一次启动本来就没有。
func (s *Store) load() error {
	unlock, err := lockConfigDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.recoverTx(); err != nil {
		return err
	}
	cfg, legacy, preserved, raw, exists, err := s.readDiskLocked()
	if err != nil {
		return err
	}
	s.cfg = cfg
	s.preservedSecrets = preserved
	if !exists || !legacy {
		return nil
	}
	backup := filepath.Join(s.dir, "config.v1.json")
	f, e := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if e == nil {
		if _, e = f.Write(raw); e != nil {
			_ = f.Close()
			_ = os.Remove(backup)
			return e
		}
		if e = f.Close(); e != nil {
			_ = os.Remove(backup)
			return e
		}
	} else if !errors.Is(e, os.ErrExist) {
		return e
	}
	nextPreserved, err := s.saveLocked(s.cfg, s.preservedSecrets)
	if err != nil {
		return err
	}
	s.preservedSecrets = nextPreserved
	return nil
}

type secretEntry struct {
	APIKey       string `json:"apiKey,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

func loadSecretsBytes(cfg *Config, b []byte) (map[string]secretEntry, error) {
	var m map[string]secretEntry
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	var preserved map[string]secretEntry
	matched := make([]bool, len(cfg.Providers))
	totalByLegacy := map[string]int{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		totalByLegacy[p.Preset+"|"+p.Label]++
		if p.ID != "" {
			if e, ok := m[p.ID]; ok {
				p.APIKey = e.APIKey
				p.AccessToken = e.AccessToken
				p.RefreshToken = e.RefreshToken
				matched[i] = true
			}
		}
	}
	for i := range cfg.Providers {
		if matched[i] {
			continue
		}
		p := &cfg.Providers[i]
		legacyKey := p.Preset + "|" + p.Label
		if e, ok := m[legacyKey]; ok {
			if totalByLegacy[legacyKey] == 1 {
				p.APIKey = e.APIKey
				p.AccessToken = e.AccessToken
				p.RefreshToken = e.RefreshToken
			} else {
				if preserved == nil {
					preserved = map[string]secretEntry{}
				}
				preserved[legacyKey] = e
			}
			continue
		}
		if e, ok := m[unclaimedSecretPrefix+legacyKey]; ok {
			if preserved == nil {
				preserved = map[string]secretEntry{}
			}
			preserved[legacyKey] = e
		}
	}
	return preserved, nil
}

func (s *Store) readDiskLocked() (Config, bool, map[string]secretEntry, []byte, bool, error) {
	txB, txOK, err := readIfExists(s.txPath())
	if err != nil {
		return Config{}, false, nil, nil, false, err
	}
	diskB, diskOK, err := readIfExists(s.path())
	if err != nil {
		return Config{}, false, nil, nil, false, err
	}
	if txOK {
		var tx configTx
		if err := json.Unmarshal(txB, &tx); err != nil {
			return Config{}, false, nil, nil, false, err
		}
		if len(tx.Config) > 0 {
			txVer, err := configBytesVersion(tx.Config)
			if err != nil {
				return Config{}, false, nil, nil, false, err
			}
			diskVer := 0
			if diskOK {
				diskVer, err = configBytesVersion(diskB)
				if err != nil {
					return Config{}, false, nil, nil, false, err
				}
			}
			if txVer > CurrentVersion && (!diskOK || txVer >= diskVer) {
				cfg, legacy, err := parse(tx.Config)
				if err != nil {
					return Config{}, false, nil, nil, false, err
				}
				var preserved map[string]secretEntry
				if txSec, hasTxSec, err := s.loadTxSecrets(tx); err != nil {
					return Config{}, false, nil, nil, false, err
				} else if hasTxSec {
					preserved, err = loadSecretsBytes(&cfg, txSec)
					if err != nil {
						return Config{}, false, nil, nil, false, err
					}
				} else if secB, secOK, err := readIfExists(s.secretPath()); err != nil {
					return Config{}, false, nil, nil, false, err
				} else if secOK {
					preserved, err = loadSecretsBytes(&cfg, secB)
					if err != nil {
						return Config{}, false, nil, nil, false, err
					}
				}
				return cfg, legacy, preserved, tx.Config, true, nil
			}
		}
	}
	if !diskOK {
		return Default(), false, nil, nil, false, nil
	}
	cfg, legacy, err := parse(diskB)
	if err != nil {
		return Config{}, false, nil, nil, false, err
	}
	var preserved map[string]secretEntry
	if secB, secOK, err := readIfExists(s.secretPath()); err != nil {
		return Config{}, false, nil, nil, false, err
	} else if secOK {
		preserved, err = loadSecretsBytes(&cfg, secB)
		if err != nil {
			return Config{}, false, nil, nil, false, err
		}
	}
	return cfg, legacy, preserved, diskB, true, nil
}

func marshalSecrets(cfg Config, preserved map[string]secretEntry) ([]byte, map[string]secretEntry, error) {
	m := map[string]secretEntry{}
	keylessCount := map[string]int{}
	for _, p := range cfg.Providers {
		if p.APIKey == "" && p.AccessToken == "" && p.RefreshToken == "" {
			keylessCount[p.Preset+"|"+p.Label]++
			continue
		}
		key := p.ID
		if key == "" {
			key = p.Preset + "|" + p.Label
		}
		m[key] = secretEntry{
			APIKey: p.APIKey, AccessToken: p.AccessToken, RefreshToken: p.RefreshToken,
		}
	}
	var nextPreserved map[string]secretEntry
	for k, e := range preserved {
		if keylessCount[k] > 0 {
			if _, exists := m[k]; !exists {
				m[unclaimedSecretPrefix+k] = e
				if nextPreserved == nil {
					nextPreserved = map[string]secretEntry{}
				}
				nextPreserved[k] = e
			}
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return b, nextPreserved, nil
}

func configBytesVersion(b []byte) (int, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return 0, err
	}
	return head.Version, nil
}

func diskConfigVersion(path string) (int, bool, error) {
	b, ok, err := readIfExists(path)
	if err != nil || !ok {
		return 0, ok, err
	}
	ver, err := configBytesVersion(b)
	if err != nil {
		return 0, false, err
	}
	return ver, true, nil
}

func txConfigVersion(path string) (int, bool, error) {
	txCfg, ok, err := readTxConfigOnly(path)
	if err != nil || !ok || len(txCfg) == 0 {
		return 0, false, err
	}
	ver, err := configBytesVersion(txCfg)
	if err != nil {
		return 0, false, err
	}
	return ver, true, nil
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
	unlock, err := lockConfigDir(s.dir)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.recoverTx(); err != nil {
		return err
	}
	if s.cfg.Version > CurrentVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, s.cfg.Version)
	}
	if diskVer, ok, err := diskConfigVersion(s.path()); err != nil {
		return err
	} else if ok && diskVer > CurrentVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, diskVer)
	}
	if txVer, ok, err := txConfigVersion(s.txPath()); err != nil {
		return err
	} else if ok && txVer > CurrentVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, txVer)
	}
	baseCfg, _, basePreserved, _, exists, err := s.readDiskLocked()
	if err != nil {
		return err
	}
	if !exists {
		baseCfg = s.cfg.clone()
		basePreserved = maps.Clone(s.preservedSecrets)
	} else if baseCfg.Version > CurrentVersion {
		return fmt.Errorf("%w: %d", ErrUnsupportedVersion, baseCfg.Version)
	}
	next := baseCfg.clone()
	fn(&next)
	normalize(&next, true)
	nextPreserved, err := s.saveLocked(next, basePreserved)
	if err != nil {
		return err
	}
	s.cfg = next
	s.preservedSecrets = nextPreserved
	return nil
}

func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func readIfExists(path string) ([]byte, bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *Store) loadTxSecrets(tx configTx) ([]byte, bool, error) {
	if len(tx.Secrets) > 0 {
		return tx.Secrets, true, nil
	}
	if tx.SecretsSHA256 == "" {
		return nil, false, nil
	}
	b, ok, err := readIfExists(s.secretTxPath())
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, errors.New("config: missing secrets tx payload")
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != tx.SecretsSHA256 {
		return nil, false, errors.New("config: secrets tx checksum mismatch")
	}
	return b, true, nil
}

func (s *Store) recoverTx() error {
	txPath := s.txPath()
	b, ok, err := readIfExists(txPath)
	if err != nil {
		return err
	}
	diskVer, diskOK, err := diskConfigVersion(s.path())
	if err != nil {
		return err
	}
	if !ok {
		if !(diskOK && diskVer > CurrentVersion) {
			_ = os.Remove(s.secretTxPath())
			_ = os.Remove(s.secretTxPath() + ".tmp")
		}
		return nil
	}
	if diskOK && diskVer > CurrentVersion {
		return nil
	}
	var tx configTx
	if err := json.Unmarshal(b, &tx); err != nil {
		return err
	}
	if len(tx.Config) > 0 {
		txVer, err := configBytesVersion(tx.Config)
		if err != nil {
			return err
		}
		if txVer > CurrentVersion {
			return nil
		}
	}
	secPayload, hasSec, err := s.loadTxSecrets(tx)
	if err != nil {
		return err
	}
	if hasSec {
		secTmp := s.secretPath() + ".tmp"
		if err := writeFileSync(secTmp, secPayload, 0o600); err != nil {
			return err
		}
		if err := os.Rename(secTmp, s.secretPath()); err != nil {
			_ = os.Remove(secTmp)
			return err
		}
	}
	if len(tx.Config) > 0 {
		cfgTmp := s.path() + ".tmp"
		if err := writeFileSync(cfgTmp, tx.Config, 0o644); err != nil {
			return err
		}
		if err := os.Rename(cfgTmp, s.path()); err != nil {
			_ = os.Remove(cfgTmp)
			return err
		}
	}
	if err := os.Remove(txPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_ = os.Remove(s.secretTxPath())
	return nil
}

func (s *Store) saveLocked(cfg Config, preserved map[string]secretEntry) (map[string]secretEntry, error) {
	cfgBytes, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	secBytes, nextPreserved, err := marshalSecrets(cfg, preserved)
	if err != nil {
		return nil, err
	}
	prevSec, prevSecExist, err := readIfExists(s.secretPath())
	if err != nil {
		return nil, err
	}
	cfgTmp := s.path() + ".tmp"
	secTmp := s.secretPath() + ".tmp"
	if err := writeFileSync(cfgTmp, cfgBytes, 0o644); err != nil {
		return nil, err
	}
	if err := writeFileSync(secTmp, secBytes, 0o600); err != nil {
		_ = os.Remove(cfgTmp)
		return nil, err
	}
	secTxPath := s.secretTxPath()
	secTxTmp := secTxPath + ".tmp"
	if err := writeFileSync(secTxTmp, secBytes, 0o600); err != nil {
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		return nil, err
	}
	if err := os.Rename(secTxTmp, secTxPath); err != nil {
		_ = os.Remove(secTxTmp)
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		return nil, err
	}
	secSum := sha256.Sum256(secBytes)
	txPath := s.txPath()
	txTmp := txPath + ".tmp"
	txBytes, err := json.Marshal(configTx{Config: cfgBytes, SecretsSHA256: hex.EncodeToString(secSum[:])})
	if err != nil {
		_ = os.Remove(secTxPath)
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		return nil, err
	}
	if err := writeFileSync(txTmp, txBytes, 0o600); err != nil {
		_ = os.Remove(secTxPath)
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		return nil, err
	}
	if err := os.Rename(txTmp, txPath); err != nil {
		_ = os.Remove(txTmp)
		_ = os.Remove(secTxPath)
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		return nil, err
	}
	if err := os.Rename(secTmp, s.secretPath()); err != nil {
		_ = os.Remove(cfgTmp)
		_ = os.Remove(secTmp)
		if rmErr := os.Remove(txPath); rmErr == nil || errors.Is(rmErr, os.ErrNotExist) {
			_ = os.Remove(secTxPath)
		}
		return nil, err
	}
	if err := os.Rename(cfgTmp, s.path()); err != nil {
		_ = os.Remove(cfgTmp)
		var rbErr error
		if prevSecExist {
			if wErr := writeFileSync(secTmp, prevSec, 0o600); wErr != nil {
				rbErr = wErr
			} else if rErr := os.Rename(secTmp, s.secretPath()); rErr != nil {
				_ = os.Remove(secTmp)
				rbErr = rErr
			}
		} else {
			if rmErr := os.Remove(s.secretPath()); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
				rbErr = rmErr
			}
		}
		if rbErr == nil {
			if rmErr := os.Remove(txPath); rmErr == nil || errors.Is(rmErr, os.ErrNotExist) {
				_ = os.Remove(secTxPath)
			}
		}
		return nil, err
	}
	if err := os.Remove(txPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_ = os.Remove(secTxPath)
	return nextPreserved, nil
}

// clone 深拷贝，并保留 nil 与空切片的区别：空切片落盘是 []，表示用户清空了列表。
func (c Config) clone() Config {
	out := c
	out.Recorder.SensitiveApps = slices.Clone(c.Recorder.SensitiveApps)
	out.Recorder.OCR.Languages = slices.Clone(c.Recorder.OCR.Languages)
	out.Providers = slices.Clone(c.Providers)
	for i := range out.Providers {
		out.Providers[i].EnabledModels = slices.Clone(out.Providers[i].EnabledModels)
		out.Providers[i].DiscoveredModels = slices.Clone(out.Providers[i].DiscoveredModels)
	}
	out.Redact.Kinds = maps.Clone(c.Redact.Kinds)
	out.Redact.Allow = slices.Clone(c.Redact.Allow)
	out.Redact.Words = slices.Clone(c.Redact.Words)
	return out
}
