package main

import "testing"

// TestNormalizeRealm 覆盖 -realm 归一化：空=自动探测、cn/intl 大小写不敏感、
// 非法值必须报错（收错版本的登录态比报错更糟）。
func TestNormalizeRealm(t *testing.T) {
	cases := []struct {
		in      string // 输入
		want    string // 期望归一化结果
		wantErr bool   // 是否应报错
	}{
		{"", "", false},           // 缺省 → 自动探测
		{"  ", "", false},         // 纯空白等同缺省
		{"cn", "cn", false},       // 国内版
		{"CN", "cn", false},       // 大小写不敏感
		{"intl", "intl", false},   // 国际版
		{"INTL", "intl", false},   // 大小写不敏感
		{" intl ", "intl", false}, // 两侧空白容忍
		{"global", "", true},      // WorkBuddy 的 realm 词不通用，拒绝
		{"xxx", "", true},         // 未知值拒绝
	}
	for _, c := range cases {
		got, err := normalizeRealm(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeRealm(%q) 应报错，实际得到 %q", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("normalizeRealm(%q) = (%q, %v)，期望 (%q, nil)", c.in, got, err, c.want)
		}
	}
}

// TestRealmLabel 中文显示名：cn/intl/空 三档都要有可读输出。
func TestRealmLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cn", "国内版"},
		{"intl", "国际版"},
		{"", "自动探测"},
	}
	for _, c := range cases {
		if got := realmLabel(c.in); got != c.want {
			t.Errorf("realmLabel(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
