package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDirFollowsCwd 回归：Dir() 必须返回当前工作目录，不能回到 exe 目录基准。
func TestDirFollowsCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir) // Go 1.24+，测试结束后自动恢复；不可与 t.Parallel 同用

	// Windows 的临时目录可能带短名/大小写差异，统一做一次符号链接归一。
	want := dir
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		want = real
	}
	if got := Dir(); !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("Dir() = %q, want 当前工作目录 %q", got, want)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got := Dir(); got != wd {
		t.Fatalf("Dir() = %q, want os.Getwd() = %q", got, wd)
	}
}

// TestResolveFollowsCwd 回归：Resolve() 不缓存基准，cwd 变化后必须跟着变，
// 否则配置（相对 cwd）与数据目录（缓存的旧基准）会再次分家。
func TestResolveFollowsCwd(t *testing.T) {
	dirA := t.TempDir()
	t.Chdir(dirA)
	wdA, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(A): %v", err)
	}
	if got, want := Resolve(filepath.Join("data", "x")), filepath.Join(wdA, "data", "x"); got != want {
		t.Fatalf("Resolve(A) = %q, want %q", got, want)
	}

	dirB := t.TempDir()
	t.Chdir(dirB)
	wdB, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(B): %v", err)
	}
	if got, want := Resolve(filepath.Join("data", "x")), filepath.Join(wdB, "data", "x"); got != want {
		t.Fatalf("Resolve(B) = %q, want %q（基准被缓存了？）", got, want)
	}
}

// TestResolvePassthrough 空串与绝对路径原样返回。
func TestResolvePassthrough(t *testing.T) {
	if got := Resolve(""); got != "" {
		t.Fatalf("Resolve(\"\") = %q, want 空串", got)
	}
	abs := filepath.Join(t.TempDir(), "abs.json")
	if got := Resolve(abs); got != abs {
		t.Fatalf("Resolve(%q) = %q, want 原样返回", abs, got)
	}
}
