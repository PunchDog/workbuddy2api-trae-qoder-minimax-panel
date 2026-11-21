// qodercapture — 在 Windows 宿主机把 Qoder 客户端登录态收进号池。
//
// 为什么需要它：面板部署在 Linux 时，客户端登录态
// （auth.v1.dat，Windows DPAPI + AES-256-GCM 加密）在 Linux 上解不开 —— DPAPI
// 是 Windows 专有机制，Linux 无法模拟。而号池 data/ide_accounts/qoder_accounts.json
// 存的是**明文快照**且已共享：在 Windows 上跑本工具收录一次，Linux 侧的面板
// 就能直接签到/续期/查额度，不必再碰客户端文件。
//
// 用法（仓库根目录，双击 qoder-capture.bat 或命令行）：
//
//	qodercapture              自动探测活跃客户端（国内/国际版取登录态较新者）
//	qodercapture -realm cn    只收国内版客户端（com.qodercn.app.stable）
//	qodercapture -realm intl  只收国际版客户端（com.qoder.app.stable）
//
// 前提：对应版本的 Qoder 客户端已在本机登录。号池落在 exe 同级
// data/ide_accounts/（与面板服务读取的 ./data 是同一份）。
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// normalizeRealm 归一化 -realm 取值：空 = 自动探测；cn/intl 大小写不敏感；
// 其余一律报错（不猜，避免收错版本的登录态）。
func normalizeRealm(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil
	case ideauth.QoderRealmCN:
		return ideauth.QoderRealmCN, nil
	case ideauth.QoderRealmINTL:
		return ideauth.QoderRealmINTL, nil
	default:
		return "", fmt.Errorf("无效 -realm %q（可选 cn|intl，缺省自动探测）", s)
	}
}

// realmLabel realm 的中文显示名（空 = 自动探测）。
func realmLabel(realm string) string {
	switch realm {
	case ideauth.QoderRealmCN:
		return "国内版"
	case ideauth.QoderRealmINTL:
		return "国际版"
	default:
		return "自动探测"
	}
}

func main() {
	realmFlag := flag.String("realm", "", "客户端版本：cn|intl（缺省自动探测活跃客户端）")
	flag.Parse()

	// 本工具的全部价值就在 Windows DPAPI 解密，非 Windows 直接说清楚。
	if runtime.GOOS != "windows" {
		fmt.Fprintf(os.Stderr, "qodercapture 只能在 Windows 上运行（当前 %s）：客户端登录态用 Windows DPAPI 加密，其它系统解不开。\n", runtime.GOOS)
		os.Exit(1)
	}
	realm, err := normalizeRealm(*realmFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qodercapture:", err)
		os.Exit(1)
	}

	snap, added, err := ideauth.UpsertQoderAccountForRealm(realm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "qodercapture: 收录失败（%s）: %v\n", realmLabel(realm), err)
		fmt.Fprintln(os.Stderr, "请确认对应版本的 Qoder 客户端已在本机登录，再重试。")
		os.Exit(1)
	}
	verb := "已更新"
	if added {
		verb = "已添加"
	}
	fmt.Printf("%s：%s <%s>（%s）\n", verb, snap.Name, snap.Email, realmLabel(snap.Realm))
	fmt.Printf("号池文件：%s\n", ideauth.PoolDir())
	fmt.Println("容器面板刷新「Qoder 账号」即可看到；签到/续期由面板自动完成。")
}
