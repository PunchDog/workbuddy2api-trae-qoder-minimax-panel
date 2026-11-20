//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigFileIsRelativeToCwd 守住配置路径契约：
// 配置文件一律相对进程当前工作目录（./config.json），不能回到「exe 所在目录」解析，
// 否则面板改的 config.json 与服务实际读取的会不是同一份。
func TestConfigFileIsRelativeToCwd(t *testing.T) {
	if filepath.IsAbs(configFile) {
		t.Fatalf("configFile 必须是相对当前工作目录的路径，实际为 %q", configFile)
	}
	if want := "config.json"; configFile != want {
		t.Fatalf("configFile = %q, want %q", configFile, want)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFile), []byte(`{"listen":":9123"}`), 0o600); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	t.Chdir(dir) // Go 1.24+，测试结束后自动恢复原工作目录

	if got := listenAddrFromConfig(configFile); got != "127.0.0.1:9123" {
		t.Fatalf("listenAddrFromConfig(%q) = %q, want 127.0.0.1:9123", configFile, got)
	}
}
