package panel

import (
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/ideauth"
)

// findEntry 按 id 取条目（测试辅助）。
func findEntry(t *testing.T, list []map[string]any, id string) map[string]any {
	t.Helper()
	for _, e := range list {
		if e["id"] == id {
			return e
		}
	}
	t.Fatalf("目录缺条目 %s", id)
	return nil
}

// TestQoderCatalogEntries 双区目录映射：前缀/倍率/档位/默认窗/峰谷价/开放徽章。
// 数据源是嵌入的官方目录快照，断言锁住映射口径（改映射必须同步改断言）。
func TestQoderCatalogEntries(t *testing.T) {
	// 直接测纯映射 + 嵌入快照（不触发实时拉取，断言与号池/网络无关）。
	cnRaw, intlRaw := qoderCatalogParse()
	cn := qoderEntriesFromCatalog(cnRaw, ideauth.QoderRealmCN)
	intl := qoderEntriesFromCatalog(intlRaw, ideauth.QoderRealmINTL)
	if len(cn) != 14 || len(intl) != 17 {
		t.Fatalf("目录条数 cn=%d intl=%d，期望 14/17（嵌入快照）", len(cn), len(intl))
	}
	// 国内版 Qwen3.8-Max：0.5×、三档思考、默认 medium、可关、默认上下文 200K。
	m := findEntry(t, cn, "qoder-cn:qmodel_38max")
	if m["credits"] != "0.5×" || m["vendor"] != "阿里云" {
		t.Fatalf("cn qmodel_38max 倍率/厂商不对: %v / %v", m["credits"], m["vendor"])
	}
	// 兼容方案锁：id 统一 qoder: 前缀（旧前端前缀筛选可用），realm 走 qrealm，
	// 名称带版本后缀（旧前端全域列表也能区分国内/国际）。
	if m["qrealm"] != ideauth.QoderRealmCN {
		t.Fatalf("cn 条目 qrealm 应为 cn: %v", m["qrealm"])
	}
	if name, _ := m["name"].(string); !strings.HasSuffix(name, "（国内版）") {
		t.Fatalf("名称应带版本后缀: %q", name)
	}
	eff, _ := m["supported_efforts"].([]string)
	if strings.Join(eff, ",") != "low,medium,xhigh" {
		t.Fatalf("思考档位排序不对: %v", eff)
	}
	if m["default_effort"] != "medium" || m["can_disable_thinking"] != true {
		t.Fatalf("默认档/可关不对: %v / %v", m["default_effort"], m["can_disable_thinking"])
	}
	if m["context_length"] != 200000 {
		t.Fatalf("默认上下文应取 is_default 窗口 200K，实际 %v", m["context_length"])
	}
	// 峰谷价未生效：只挂徽章文案，不冒充现价倍率。
	label, _ := m["promo_label"].(string)
	if !strings.Contains(label, "折") || !strings.Contains(label, "未生效") {
		t.Fatalf("未生效峰谷价应带徽章+未生效标注: %q", label)
	}
	if _, has := m["promo_factor"]; has {
		t.Fatal("未生效促销不应给 promo_factor")
	}
	// 免费模型 0×；低档位排序 low 在前。
	if f := findEntry(t, cn, "qoder-cn:qfmodel"); f["credits"] != "0×" {
		t.Fatalf("qfmodel 应 0×，实际 %v", f["credits"])
	}
	df := findEntry(t, cn, "qoder-cn:dfmodel")
	eff2, _ := df["supported_efforts"].([]string)
	if strings.Join(eff2, ",") != "low,high,max" {
		t.Fatalf("dfmodel 档位排序不对: %v", eff2)
	}
	// 国际版 Ultimate：2×、未开放徽章、五档思考。
	u := findEntry(t, intl, "qoder-intl:ultimate")
	if u["credits"] != "2×" || u["not_enabled"] != true {
		t.Fatalf("intl ultimate 倍率/开放徽章不对: %v / %v", u["credits"], u["not_enabled"])
	}
	if u["qrealm"] != ideauth.QoderRealmINTL {
		t.Fatalf("intl 条目 qrealm 应为 intl: %v", u["qrealm"])
	}
	// 国际版默认模型是 qmodel_38max（cn 的默认是 auto）。
	if d := findEntry(t, intl, "qoder-intl:qmodel_38max"); d["is_default"] != true {
		t.Fatal("intl 默认模型应为 qmodel_38max")
	}
	if d := findEntry(t, cn, "qoder-cn:auto"); d["is_default"] != true {
		t.Fatal("cn 默认模型应为 auto")
	}
}

// TestQoderRealmsFromViews 挂载门的聚合逻辑（纯函数）：去重、cn 在前、
// 空 Realm 老账号回落 intl。号池读取接线由 ListQoderAccounts 承担（既有测试覆盖）。
func TestQoderRealmsFromViews(t *testing.T) {
	cases := []struct {
		name  string
		views []ideauth.QoderAccountView
		want  string // 逗号连接的期望序列
	}{
		{"空号池", nil, ""},
		{"双区去重 cn 在前", []ideauth.QoderAccountView{
			{Realm: ideauth.QoderRealmINTL}, {Realm: ideauth.QoderRealmCN}, {Realm: ideauth.QoderRealmCN},
		}, "cn,intl"},
		{"仅国内版", []ideauth.QoderAccountView{{Realm: ideauth.QoderRealmCN}}, "cn"},
		{"老账号无 Realm 回落 intl", []ideauth.QoderAccountView{{}, {Realm: ideauth.QoderRealmCN}}, "cn,intl"},
	}
	for _, c := range cases {
		got := qoderRealmsFromViews(c.views)
		if strings.Join(got, ",") != c.want {
			t.Errorf("%s: realms = %v，期望 %q", c.name, got, c.want)
		}
	}
}
