// Package appdir 统一解析「程序自己的目录」。
//
// 为什么需要：分发给别人后用户可能是双击 exe、用 start-panel.bat（脚本会先
// cd /d 到项目根目录）或从快捷方式启动（工作目录可能是 C:\Windows\System32）。
// 若按启动者的 cwd 解析相对路径，config.json、data/ 下的 pid/log 与号池就会
// 各读各的、写到莫名其妙的地方，表现为「面板改了配置但服务读的是另一份」。
//
// 因此这里以 exe 所在目录为唯一基准：启动时 cmd/app 调用 Chdir() 把 cwd 切到
// exe 目录，于是 config.json（./config.json）、data/（pid/log）与号池数据
// （Resolve）统一落在 exe 旁，配置与数据不分家。
//
// 注意：exe 需放在可写目录；若所在目录只读，配置与号池都会写入失败。
// Docker 走 cmd/server + WORKDIR /app + 显式 -config /app/config.json，
// 不经过本入口，绝对路径优先，不受 Chdir 影响。
package appdir

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	once sync.Once
	dir  string
)

// Dir 返回程序所在目录（exe 同级）。
// go run / go test 场景下 exe 落在临时构建目录里，此时回落当前工作目录。
func Dir() string {
	once.Do(func() {
		if p, err := os.Executable(); err == nil {
			if real, lerr := filepath.EvalSymlinks(p); lerr == nil {
				p = real
			}
			d := filepath.Dir(p)
			if d != "" && !isTempBuildDir(d) {
				dir = d
				return
			}
		}
		wd, err := os.Getwd()
		if err != nil || wd == "" {
			wd = "."
		}
		dir = wd
	})
	return dir
}

// isTempBuildDir 判断是否 go run / go test 的临时构建目录。
func isTempBuildDir(d string) bool {
	lower := strings.ToLower(filepath.Clean(d))
	if strings.Contains(lower, "go-build") {
		return true
	}
	if t := os.TempDir(); t != "" {
		if strings.HasPrefix(lower, strings.ToLower(filepath.Clean(t))) {
			return true
		}
	}
	return false
}

// Resolve 把相对路径按程序目录解析成绝对路径；绝对路径与空串原样返回。
func Resolve(p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(Dir(), p)
}

// Chdir 把当前工作目录切到程序目录。
//
// 服务内部仍有少量相对路径（config 里的 ./auths、./data/state.json 等），
// 启动时统一对齐到 exe 目录，可以一次性兜住这些历史写法。
func Chdir() error {
	return os.Chdir(Dir())
}
