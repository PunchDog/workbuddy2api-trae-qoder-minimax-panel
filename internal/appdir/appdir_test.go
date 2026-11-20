package appdir

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// samePath 在 Windows 上忽略盘符/路径大小写差异，统一比较规范化后的路径。
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// TestDirFallsBackToCwdInGoBuildDir 验证 go test 临时构建目录下的回落分支：
// 测试二进制本身位于 go-build 临时目录时，Dir() 必须回落 cwd。
//
// 注意：Dir() 带 sync.Once 缓存，本进程内只由本用例消费一次基准。
// exe 锚定的正面断言放在下面的子进程探针里完成，避免回落逻辑掩盖断言。
func TestDirFallsBackToCwdInGoBuildDir(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	exeDir := filepath.Dir(exe)
	if !isTempBuildDir(exeDir) {
		t.Skipf("测试二进制不在临时构建目录（%q），跳过回落断言", exeDir)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	if got := Dir(); !samePath(got, wd) {
		t.Fatalf("Dir() = %q, want 回落当前工作目录 %q", got, wd)
	}
	if got, want := Resolve(filepath.Join("data", "x")), filepath.Join(wd, "data", "x"); !samePath(got, want) {
		t.Fatalf("Resolve(data/x) = %q, want %q", got, want)
	}
}

// TestResolvePassthrough 空串与绝对路径原样返回，与基准无关。
func TestResolvePassthrough(t *testing.T) {
	if got := Resolve(""); got != "" {
		t.Fatalf("Resolve(\"\") = %q, want 空串", got)
	}
	abs := filepath.Join(t.TempDir(), "abs.json")
	if got := Resolve(abs); got != abs {
		t.Fatalf("Resolve(%q) = %q, want 原样返回", abs, got)
	}
}

const appdirProbeEnv = "WB_APPDIR_PROBE_CHILD"

// TestExeAnchoredProbe 验证 exe 锚定语义。
//
// 直接在本测试进程里断言「Dir() == exe 目录」是假守卫：go test 的二进制在
// go-build 临时目录，isTempBuildDir 会让它回落 cwd。这里把测试二进制编译到
// 用户主目录下的非临时目录（只读源码的 CI 也能跑），再以不同 cwd 启动子进程：
// 子进程里 exe 目录是真实基准，Chdir/Dir/Resolve 都会被实证。
func TestExeAnchoredProbe(t *testing.T) {
	if os.Getenv(appdirProbeEnv) == "1" {
		testExeAnchoredChild(t)
		return
	}

	pkgDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Fatalf("os.UserHomeDir: %v（home=%q）", err, home)
	}
	probeDir, err := os.MkdirTemp(home, ".wb2api-appdir-probe-")
	if err != nil {
		t.Fatalf("在主目录创建探针目录失败: %v", err)
	}
	defer func() { _ = os.RemoveAll(probeDir) }()
	if isTempBuildDir(probeDir) {
		t.Fatalf("探针目录 %q 被判为临时构建目录（TEMP=%q），无法验证 exe 锚定", probeDir, os.TempDir())
	}

	probeExe := filepath.Join(probeDir, "appdir_probe.exe")
	build := exec.Command("go", "test", "-c", "-o", probeExe, ".")
	build.Dir = pkgDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("编译探针测试二进制失败: %v\n%s", err, out)
	}

	// 以与 exe 目录不同的 cwd 启动，模拟 start-panel.bat 从项目根拉起 exe。
	run := exec.Command(probeExe, "-test.run=^TestExeAnchoredProbe$", "-test.v")
	run.Dir = pkgDir
	run.Env = append(os.Environ(), appdirProbeEnv+"=1")
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("探针子进程失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "APPDIR_PROBE_OK") {
		t.Fatalf("探针子进程未输出 APPDIR_PROBE_OK：\n%s", out)
	}
	t.Logf("appdir 探针输出：\n%s", out)
}

// testExeAnchoredChild 只在探针子进程中执行，直接断言 exe 目录基准。
func testExeAnchoredChild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if real, lerr := filepath.EvalSymlinks(exe); lerr == nil {
		exe = real
	}
	exeDir := filepath.Dir(exe)
	if isTempBuildDir(exeDir) {
		t.Fatalf("探针 exe 位于临时构建目录 %q，无法验证 exe 锚定", exeDir)
	}

	cwdBefore, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(before): %v", err)
	}
	if samePath(cwdBefore, exeDir) {
		t.Fatalf("探针前置条件失败：cwd 与 exe 目录相同（%q）", cwdBefore)
	}

	if got := Dir(); !samePath(got, exeDir) {
		t.Fatalf("Dir() = %q, want exe 目录 %q", got, exeDir)
	}
	if err := Chdir(); err != nil {
		t.Fatalf("Chdir(): %v", err)
	}
	cwdAfter, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(after): %v", err)
	}
	if !samePath(cwdAfter, exeDir) || samePath(cwdAfter, cwdBefore) {
		t.Fatalf("Chdir() 后 cwd = %q, want exe 目录 %q（原 cwd %q）", cwdAfter, exeDir, cwdBefore)
	}
	if got, want := Resolve(filepath.Join("data", "x")), filepath.Join(exeDir, "data", "x"); !samePath(got, want) {
		t.Fatalf("Resolve(data/x) = %q, want %q", got, want)
	}
	if got := Resolve(""); got != "" {
		t.Fatalf("Resolve(\"\") = %q, want 空串", got)
	}
	abs := filepath.Join(exeDir, "abs.json")
	if got := Resolve(abs); got != abs {
		t.Fatalf("Resolve(绝对路径) = %q, want %q", got, abs)
	}
	t.Logf("APPDIR_PROBE_OK exeDir=%s cwdBefore=%s cwdAfter=%s", exeDir, cwdBefore, cwdAfter)
}
