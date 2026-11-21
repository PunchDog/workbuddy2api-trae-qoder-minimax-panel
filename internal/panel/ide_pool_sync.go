package panel

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// idePoolSyncInterval 是「IDE 号池 → 网关主池」惰性同步的最小间隔。
//
// 为什么需要节流：overview / 签到池会被前端多路轮询，每次都解析 16KB+ 号池文件
// （含 authFileRaw 原文）没必要；账号登录是低频动作，30s 粒度足够。
const idePoolSyncInterval = 30 * time.Second

// idePoolSyncedAt 上次同步时刻（UnixNano，0 = 从未同步）。
var idePoolSyncedAt atomic.Int64

// syncIDEWorkBuddyToMainPool 把 IDE 号池（data/ide_accounts/workbuddy_accounts.json）
// 里已登录的 WorkBuddy 账号投影到网关主池（auths/workbuddy-<uid>.json + Pool）。
//
// 为什么必须投影：浏览器登录流程只写 IDE 号池快照；而 /panel/api/overview 的
// accounts、签到、余额、选号、冷却全走主池 *auth.Auth。两张表不通时，用户看到
// 「登录成功、号池文件也有账号」但面板账号数 / 积分恒为 0，网关也选不到该号。
//
// force=true 跳过节流（登录成功后立即调用，让 UI 马上看到账号）。
// 返回本次新投影的账号数。
func (p *Panel) syncIDEWorkBuddyToMainPool(force bool) int {
	if p == nil || p.cfg.Pool == nil {
		return 0
	}
	now := time.Now().UnixNano()
	if !force {
		last := idePoolSyncedAt.Load()
		if last != 0 && now-last < int64(idePoolSyncInterval) {
			return 0
		}
	}
	idePoolSyncedAt.Store(now)

	f, err := ideauth.LoadWorkBuddyPool()
	if err != nil {
		log.Printf("panel: 读取 IDE WorkBuddy 号池失败，跳过主池同步: %v", err)
		return 0
	}
	if len(f.Accounts) == 0 {
		return 0
	}
	dir := strings.TrimSpace(p.cfg.AuthDir)
	if dir == "" {
		dir = "./auths"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("panel: 创建 auths 目录失败，跳过主池同步: %v", err)
		return 0
	}

	added := 0
	var addedUIDs []string
	for _, snap := range f.Accounts {
		if snap == nil || snap.Account == nil || snap.Auth == nil {
			continue
		}
		uid := strings.TrimSpace(snap.Account.UID)
		if !validImportUID(uid) || strings.TrimSpace(snap.Auth.AccessToken) == "" {
			continue
		}
		if p.cfg.Pool.AuthByUID(uid) != nil {
			continue // 主池已有：保留主池侧刷新后的 token，不反向覆盖
		}
		a := &auth.Auth{
			AccessToken:  snap.Auth.AccessToken,
			RefreshToken: snap.Auth.RefreshToken,
			ExpiresAt:    toUnixSeconds(snap.Auth.ExpiresAt),
			Domain:       snap.Auth.Domain,
			UID:          uid,
			Nickname:     snap.Account.Nickname,
			EnterpriseID: snap.Account.EnterpriseID,
			FilePath:     filepath.Join(dir, fmt.Sprintf("workbuddy-%s.json", uid)),
		}
		// realm 以 IDE 号池的 edition 为准：intl/intlbuddy 属 global 域，
		// cn 属国内域；显式落盘，不依赖 domain 后缀推断（domain 常为空）。
		realm := "cn"
		if snap.Edition != string(ideauth.WorkBuddyCN) {
			realm = "global"
		}
		if _, err := auth.BackfillRealmFor(a, realm); err != nil {
			log.Printf("panel: uid=%s 写入 realm 失败: %v", uid, err)
			continue
		}
		if err := a.SaveAtomic(); err != nil {
			log.Printf("panel: uid=%s 写 auth 文件失败: %v", uid, err)
			continue
		}
		p.cfg.Pool.Add(a)
		addedUIDs = append(addedUIDs, uid)
		added++
	}
	if added > 0 {
		log.Printf("panel: IDE 号池投影到主池 %d 个 WorkBuddy 账号", added)
		// 新投影账号的余额未知（状态里是 0）：用户刚登录完看到「有积分却显示 0」
		// 就是这里空窗——下一次定时刷新最多要等 5 分钟。后台立刻补一次余额查询。
		p.refreshCreditsAsync(addedUIDs)
	}
	return added
}

// refreshCreditsAsync 为新加入池的账号异步拉一次余额并写回池状态。
//
// 不阻塞调用方（overview 是高频轮询接口）；失败只记日志：余额保持未知（0），
// 下一次定时刷新会再试，绝不因此罚号。
func (p *Panel) refreshCreditsAsync(uids []string) {
	if len(uids) == 0 || p.cfg.Upstream == nil {
		return
	}
	go func() {
		for _, uid := range uids {
			a := p.cfg.Pool.AuthByUID(uid)
			if a == nil {
				continue
			}
			remain, total, _, err := p.cfg.Upstream.UserResourceDetailed(a, 0)
			if err != nil {
				log.Printf("panel: uid=%s 首次余额查询失败（等定时刷新重试）: %v", uid, err)
				continue
			}
			p.cfg.Pool.SetCredits(uid, remain, total)
			p.cfg.Pool.ReenableIfCredits(uid, remain, total)
		}
	}()
}

// toUnixSeconds 把 IDE 号池的毫秒时间戳归一成 auth.Auth 的秒口径。
func toUnixSeconds(v int64) int64 {
	if v <= 0 {
		return 0
	}
	if v > 1e12 {
		return v / 1000
	}
	return v
}
