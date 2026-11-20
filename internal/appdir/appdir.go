// Package appdir 统一解析数据目录路径。
//
// 基准统一为进程当前工作目录（cwd），与 config.json（./config.json）同基准：
// 配置、号池、用量等运行期文件要么一起落在启动目录，要么一起落在容器 WORKDIR，
// 不会出现「面板改了 ./config.json、号池却写进 exe 所在目录」的双基准分家。
//
// 因此从快捷方式启动桌面 exe 时，请把「起始位置/工作目录」固定为 exe 目录或
// 某个固定可写目录（Docker 用 WORKDIR /app），确保 cwd 有写权限；
// 若落在只读目录（如 C:\Windows\System32），配置与号池都会写入失败。
package appdir

import (
	"os"
	"path/filepath"
)

// Dir 返回当前工作目录（获取失败时回落 "."）。
//
// 每次调用都读 os.Getwd()，不做缓存：config.json 也是相对 cwd 逐次解析的，
// 一旦缓存就可能在 cwd 变化后再次与配置分家。
func Dir() string {
	wd, err := os.Getwd()
	if err != nil || wd == "" {
		return "."
	}
	return wd
}

// Resolve 把相对路径按当前工作目录解析成绝对路径；绝对路径与空串原样返回。
func Resolve(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(Dir(), p)
}

// Chdir 保留仅为兼容旧调用方：cwd 本身就是基准，无需切换。
func Chdir() error {
	return nil
}
