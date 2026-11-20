//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	// 三个监听地址互不相同：一旦 main() 未 Chdir 到 exe 目录，
	// 无 -config 的进程会读 cwd 那份配置并监听 listenCwd，断言立即失败。
	listenExe = "127.0.0.1:19711"
	listenCwd = "127.0.0.1:19712"
	listenAbs = "127.0.0.1:19713"
	bootWait  = 20 * time.Second
)

// TestConfigReadsExeDirProbe 是端到端集成测试：构建真实的 cmd/app exe，
// 从「项目根」cwd 启动它，验证默认读取 exe 旁 config.json、显式 -config
// 绝对路径优先。
//
// 关键：被验证的是 main() 入口的 appdir.Chdir() 接线。测试进程自己绝不调用
// Chdir，也不直接调 listenAddrFromConfig 断言 —— 那样测试二进制跑的是 testing
// main，误删 main() 里的 Chdir 仍会全绿（QA 复现的 P1 假守卫）。
func TestConfigReadsExeDirProbe(t *testing.T) {
	if filepath.IsAbs(configFile) || configFile != "config.json" {
		t.Fatalf("configFile 必须是相对路径 \"config.json\"，实际为 %q", configFile)
	}

	repoRoot := repoRootFromCaller(t)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("os.UserHomeDir: %v（home=%q）", err, home)
	}
	probeRoot, err := os.MkdirTemp(home, ".wb2api-probe-")
	if err != nil {
		t.Fatalf("在主目录创建探针目录失败: %v", err)
	}
	defer func() { _ = os.RemoveAll(probeRoot) }()
	// 若 home 恰在系统临时目录下，isTempBuildDir 会让 appdir.Dir() 回落 cwd，
	// 此时探针无法区分 exe 与 cwd 基准，必须直接判失败而不是给出假绿。
	if isTempLikeDir(probeRoot) {
		t.Fatalf("探针目录 %q 落在临时/构建目录下（TEMP=%q），无法验证 exe 锚定", probeRoot, os.TempDir())
	}

	exeDir := filepath.Join(probeRoot, "dist")
	cwdDir := filepath.Join(probeRoot, "project-root")
	mustMkdir(t, exeDir)
	mustMkdir(t, cwdDir)

	// 布局模拟 start-panel.bat：exe 在 dist/，进程 cwd 在「项目根」。
	exe := filepath.Join(exeDir, "WorkBuddy2API.exe")
	writeProbeConfig(t, filepath.Join(exeDir, configFile), listenExe)
	writeProbeConfig(t, filepath.Join(cwdDir, configFile), listenCwd)
	absCfg := filepath.Join(cwdDir, "absolute-config.json")
	writeProbeConfig(t, absCfg, listenAbs)

	// 构建真实入口；-tags server 避开 Wails/CGO，main() 的 Chdir 仍会执行。
	build := exec.Command("go", "build", "-trimpath", "-tags", "server", "-o", exe, "./cmd/app")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建真实 cmd/app exe 失败: %v\n%s", err, out)
	}

	for _, addr := range []string{listenExe, listenCwd, listenAbs} {
		if portReady(addr) {
			t.Fatalf("探针前置条件失败：端口 %s 已被占用，请换端口后重跑", addr)
		}
	}

	// 场景 1：无 -config（start-panel.bat 场景）→ 必须读 exe 旁 config。
	runServerScenario(t, exe, cwdDir, []string{"--server"}, listenExe, []string{listenCwd})

	// 场景 2：显式绝对路径 -config → 优先级高于 exe 旁与 cwd 两份 config。
	runServerScenario(t, exe, cwdDir, []string{"--server", "-config", absCfg}, listenAbs, []string{listenExe, listenCwd})

	if portReady(listenExe) || portReady(listenCwd) || portReady(listenAbs) {
		t.Fatalf("探针结束后仍有 LISTENING 残留：%s / %s / %s", listenExe, listenCwd, listenAbs)
	}
}

// runServerScenario 启动真实 exe、轮询端口、断言后强制回收进程。
func runServerScenario(t *testing.T, exe, cwd string, args []string, want string, notWant []string) {
	t.Helper()

	cmd := exec.Command(exe, args...)
	cmd.Dir = cwd
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动探针 exe 失败（args=%v）: %v", args, err)
	}
	stopped := false
	stop := func() {
		if !stopped {
			stopped = true
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}
	defer stop()

	deadline := time.Now().Add(bootWait)
	for time.Now().Before(deadline) && !portReady(want) {
		time.Sleep(200 * time.Millisecond)
	}
	wantReady := portReady(want)
	unexpected := []string{}
	for _, addr := range notWant {
		if portReady(addr) {
			unexpected = append(unexpected, addr)
		}
	}

	stop() // 先回收进程，再读输出，避免与子进程写入产生并发读。
	waitPortGone(want, 5*time.Second)
	log := out.String()
	t.Logf("场景 args=%v cwd=%s want=%s ready=%v notWant=%v 出现=%v\n日志:\n%s",
		args, cwd, want, wantReady, notWant, unexpected, log)

	if !wantReady {
		t.Fatalf("期望监听 %s 未就绪（exe=%s, cwd=%s，说明读错了 config）。日志:\n%s", want, exe, cwd, log)
	}
	if len(unexpected) > 0 {
		t.Fatalf("这些地址不应被监听：%v。日志:\n%s", unexpected, log)
	}
	if !strings.Contains(log, want) {
		t.Fatalf("日志中未出现监听地址 %s，无法确认配置来源。日志:\n%s", want, log)
	}
}

// repoRootFromCaller 用源码位置定位仓库根，不依赖测试进程的 cwd。
func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller 失败，无法定位仓库根")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatalf("定位仓库根失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("仓库根 %q 下找不到 go.mod: %v", root, err)
	}
	return root
}

// isTempLikeDir 复刻 appdir.isTempBuildDir 的判定口径（跨包不可直接调用）。
func isTempLikeDir(d string) bool {
	lower := strings.ToLower(filepath.Clean(d))
	if strings.Contains(lower, "go-build") {
		return true
	}
	if tmp := os.TempDir(); tmp != "" && strings.HasPrefix(lower, strings.ToLower(filepath.Clean(tmp))) {
		return true
	}
	return false
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("创建目录 %s 失败: %v", path, err)
	}
}

func writeProbeConfig(t *testing.T, path, listen string) {
	t.Helper()
	content := fmt.Sprintf(`{"listen":%q,"api_key":"sk-probe"}`, listen)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置 %s 失败: %v", path, err)
	}
}

// portReady 直接复用 main.go 的同名函数（同包），避免测试另起一套端口探测口径。

func waitPortGone(addr string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !portReady(addr) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
