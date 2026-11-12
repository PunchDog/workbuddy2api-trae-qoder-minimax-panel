//go:build !windows

// win_other.go 非 Windows 平台的对应实现（Windows 版见 win.go）。
//
// 存在的理由与 crypto_common.go 相同：win.go 曾缺少 build tag，导致 linux/darwin
// 交叉编译失败。本项目主要面向 Windows，非 Windows 只需要「能编过 + 行为可预期」：
//   - OpenURL：走 xdg-open（Linux）/ open（macOS）；
//   - DPAPIUnprotect / QoderOSCryptKey：Windows 专有（DPAPI），非 Windows 直接报错；
//   - QoderDataDir：按 XDG 目录约定推导；
//   - runHidden：无需隐藏窗口，直接执行。
package ideauth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// OpenURL 用系统默认浏览器打开 URL（非 Windows：xdg-open / open）。
func OpenURL(u string) error {
	if u == "" {
		return errors.New("URL 为空")
	}
	var opener string
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	default:
		opener = "xdg-open"
	}
	cmd := exec.Command(opener, u)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// DPAPIUnprotect 非 Windows 无 DPAPI，无法解密。
func DPAPIUnprotect(data []byte) ([]byte, error) {
	return nil, errors.New("DPAPI 解密仅 Windows 可用")
}

// Qoder 国际版 / 国内版桌面客户端的目录名（非 Windows：Linux ~/.config/、macOS ~/Library/Application Support/）。
const (
	qoderAppDirIntl = "com.qoder.app.stable"
	qoderAppDirCN   = "com.qodercn.app.stable"
)

// qoderDetectAppDir 在 parent 目录下探测真实存在的 Qoder 客户端目录（非 Windows）。
func qoderDetectAppDir(parent string) string {
	intlDir := filepath.Join(parent, qoderAppDirIntl)
	cnDir := filepath.Join(parent, qoderAppDirCN)
	intlOK := dirExists(intlDir)
	cnOK := dirExists(cnDir)
	switch {
	case intlOK && cnOK:
		intlM, _ := authFileModTime(intlDir)
		cnM, _ := authFileModTime(cnDir)
		if cnM.After(intlM) {
			return qoderAppDirCN
		}
		return qoderAppDirIntl
	case cnOK:
		return qoderAppDirCN
	default:
		return qoderAppDirIntl
	}
}

// dirExists 判断路径是否为已存在目录。
func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// authFileModTime 返回目录下 auth.v1.dat 的修改时间；文件不存在返回零值 + false。
func authFileModTime(dir string) (time.Time, bool) {
	st, err := os.Stat(filepath.Join(dir, qoderAuthFile))
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}

// QoderDataDir 按 XDG 目录约定推导 Qoder 数据目录（非 Windows）。
// 优先级：环境变量 QODER_DATA_DIR > XDG_CONFIG_HOME > ~/.config 下自动探测 > 默认国际版。
func QoderDataDir() string {
	if v := os.Getenv("QODER_DATA_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, qoderDetectAppDir(v))
	}
	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".config")
	return filepath.Join(base, qoderDetectAppDir(base))
}

// QoderDataDirForRealm 返回指定 realm 的 Qoder 数据目录（非 Windows，不自动探测）。
func QoderDataDirForRealm(realm string) string {
	if v := os.Getenv("QODER_DATA_DIR"); v != "" {
		return v
	}
	dir := qoderAppDirIntl
	if realm == QoderRealmCN {
		dir = qoderAppDirCN
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, dir)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", dir)
}

// QoderOSCryptKey 依赖 DPAPI，非 Windows 不可用。
func QoderOSCryptKey() ([]byte, error) {
	return qoderOSCryptKeyInDir(QoderDataDir())
}

// qoderOSCryptKeyInDir 非 Windows 桩：DPAPI 是 Windows 专有机制，Linux/容器里
// 无法解密客户端登录态。报错直接给出路（浏览器授权登录 / 宿主机收录工具），
// 别让用户对着「依赖 Windows DPAPI」干瞪眼。
func qoderOSCryptKeyInDir(dir string) ([]byte, error) {
	return nil, fmt.Errorf("OSCrypt key 读取依赖 Windows DPAPI（当前系统 %s 不支持）——请改用面板「浏览器授权登录」（容器可用），或在 Windows 宿主机运行 qoder-capture.bat 收录账号", runtime.GOOS)
}

// runHidden 执行命令并返回 stdout（非 Windows 无需隐藏窗口）。
func runHidden(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// hideWindow 非 Windows 无窗口隐藏概念，空实现。
func hideWindow(cmd *exec.Cmd) {}
