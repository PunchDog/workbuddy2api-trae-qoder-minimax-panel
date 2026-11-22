// Package modelswitch 管理模型级启用开关台账。
//
// 数据落 data/model_switches.json（独立于需重启的 config.json），内存镜像供 O(1) 即时读取，
// 写入走临时文件 + rename 原子替换防半写。台账缺失或损坏一律回退「默认全开」，不阻塞网关启动。
// 契约：File.Models 中显式记录的 id 覆盖 Default；未记录的 id 取 Default。
// 写入保留所有显式项（含 true）以便审计，读取时按 id 精确查表（不按前缀豁免）。
package modelswitch

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// SchemaVersion 台账格式版本，便于后续演进。
	SchemaVersion = 1
	// DefaultEnabled 默认全开：台账为空时所有模型可见，避免上线瞬间模型全灭。
	DefaultEnabled = true
)

// File 是 data/model_switches.json 的磁盘结构。
type File struct {
	Version int             `json:"version"`
	Default bool            `json:"default"`
	Models  map[string]bool `json:"models"`
}

// Store 是开关台账的内存镜像 + 原子落盘封装，并发安全。
type Store struct {
	mu     sync.RWMutex
	path   string
	def    bool
	models map[string]bool
}

// New 从 path 加载台账。文件不存在视为默认全开；JSON 损坏时保留副本并回退默认全开。
func New(path string) *Store {
	s := &Store{path: path, def: DefaultEnabled, models: make(map[string]bool)}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[modelswitch] 读取台账失败，回退默认全开: path=%s err=%v", path, err)
		}
		return s
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		s.quarantine(raw, err)
		return s
	}
	if f.Models != nil {
		s.def = f.Default
		s.models = f.Models
	}
	return s
}

// quarantine 保留损坏文件副本便于排查，然后回退默认全开。
func (s *Store) quarantine(raw []byte, parseErr error) {
	log.Printf("[modelswitch] 台账 JSON 损坏，回退默认全开: path=%s err=%v", s.path, parseErr)
	backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405.000"))
	if err := os.WriteFile(backup, raw, 0o600); err != nil {
		log.Printf("[modelswitch] 保存损坏副本失败: path=%s err=%v", backup, err)
	}
}

// Path 返回台账文件路径。
func (s *Store) Path() string { return s.path }

// Enabled 读取某个调用值 id 的启用状态；未记录时返回 Default。
func (s *Store) Enabled(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.models[id]; ok {
		return v
	}
	return s.def
}

// Snapshot 返回默认值与显式记录项的副本，供接口只读输出（调用方修改不影响台账）。
func (s *Store) Snapshot() (def bool, models map[string]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]bool, len(s.models))
	for k, v := range s.models {
		out[k] = v
	}
	return s.def, out
}

// Set 批量更新开关，先在锁内构造新镜像并原子落盘，成功后才切换内存镜像。
// 返回实际写入的条目数（空 id 被忽略）。落盘失败时内存保持不变。
func (s *Store) Set(updates map[string]bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	newModels := make(map[string]bool, len(s.models)+len(updates))
	for k, v := range s.models {
		newModels[k] = v
	}
	applied := 0
	for id, v := range updates {
		if id == "" {
			continue
		}
		newModels[id] = v
		applied++
	}
	if applied == 0 {
		return 0, nil
	}
	raw, err := json.MarshalIndent(File{
		Version: SchemaVersion,
		Default: s.def,
		Models:  newModels,
	}, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := s.writeAtomic(raw); err != nil {
		return 0, err
	}
	s.models = newModels
	return applied, nil
}

// writeAtomic 临时文件 + rename 原子替换，避免写入中途崩溃留下半写文件。
func (s *Store) writeAtomic(raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
