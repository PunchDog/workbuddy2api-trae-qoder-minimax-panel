package appserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureAPIKeyGeneratesAndPersists 未填 api_key：生成 sk- 密钥、写回文件、
// 保留用户手写未知键；Config 内存值同步更新（调用鉴权立即用它）。
func TestEnsureAPIKeyGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	orig := `{"listen":":7863","api_key":"","my_custom_key":{"a":1}}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Default()
	key, err := EnsureAPIKey(c, path)
	if err != nil {
		t.Fatalf("EnsureAPIKey: %v", err)
	}
	if !strings.HasPrefix(key, "sk-") || len(key) < 20 {
		t.Fatalf("密钥形态不对: %q", key)
	}
	if c.APIKey != key {
		t.Fatalf("内存 Config 未同步: %q != %q", c.APIKey, key)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		t.Fatalf("写回后不是合法 JSON: %s", raw)
	}
	if m["api_key"] != key {
		t.Fatalf("文件 api_key 未写回: %v", m["api_key"])
	}
	if _, ok := m["my_custom_key"]; !ok {
		t.Fatal("用户手写未知键被丢弃")
	}
	if m["listen"] != ":7863" {
		t.Fatalf("既有键被改动: %v", m["listen"])
	}
	// 二次启动：已填写 → 不再生成、不再改文件。
	c2 := Default()
	c2.APIKey = key
	key2, err := EnsureAPIKey(c2, path)
	if err != nil || key2 != "" {
		t.Fatalf("已填写应原样放行: key2=%q err=%v", key2, err)
	}
	raw2, _ := os.ReadFile(path)
	if string(raw2) != string(raw) {
		t.Fatal("已填写时不应改写文件")
	}
}

// TestEnsureAPIKeyNoPath path 空（Load("") 兜底形态）：只生成内存 key，不碰盘。
func TestEnsureAPIKeyNoPath(t *testing.T) {
	c := Default()
	key, err := EnsureAPIKey(c, "")
	if err != nil || !strings.HasPrefix(key, "sk-") || c.APIKey != key {
		t.Fatalf("内存生成不对: key=%q err=%v", key, err)
	}
}

// TestEnsureAPIKeyWriteBackFail 写回失败（路径是目录）：仍返回可用 key + err，
// 调用方记日志后用内存 key 继续启动，不阻塞服务。
func TestEnsureAPIKeyWriteBackFail(t *testing.T) {
	dir := t.TempDir()
	c := Default()
	key, err := EnsureAPIKey(c, dir)
	if err == nil {
		t.Fatal("目录路径写回应报错")
	}
	if !strings.HasPrefix(key, "sk-") || c.APIKey != key {
		t.Fatalf("写回失败仍应给内存 key: %q", key)
	}
}

// TestGenerateAPIKeyUnique 两次生成不重复（随机熵足够）。
func TestGenerateAPIKeyUnique(t *testing.T) {
	a, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !strings.HasPrefix(a, "sk-") || !strings.HasPrefix(b, "sk-") {
		t.Fatalf("密钥应唯一且 sk- 前缀: %q / %q", a, b)
	}
}
