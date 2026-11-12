package server

import "testing"

// TestResolveModelQoder 路由前缀协议：qoder-cn/qoder-intl 钉死区域、裸 qoder 别名、
// 未知前缀回落 cn 裸名（零回归）。双区同 key 模型靠显式前缀区分出口区域。
func TestResolveModelQoder(t *testing.T) {
	cases := []struct {
		in        string
		wantRealm string
		wantBare  string
	}{
		{"qoder-cn:qmodel_latest", "qoder-cn", "qmodel_latest"},
		{"qoder-intl:qmodel_latest", "qoder-intl", "qmodel_latest"},
		{"qoder:auto", "qoder", "auto"},
		{"cn:glm-5.2", "cn", "glm-5.2"},
		{"global:glm-5.2", "global", "glm-5.2"},
		{"trae:x", "trae", "x"},
		{"glm-5.2", "cn", "glm-5.2"},        // 裸名零回归
		{"unknown:x", "cn", "unknown:x"},    // 未知前缀不剥离
		{"qoder-cn:a:b", "qoder-cn", "a:b"}, // 只剥第一个冒号
	}
	for _, c := range cases {
		r, b := resolveModel(c.in)
		if r != c.wantRealm || b != c.wantBare {
			t.Errorf("resolveModel(%q) = (%q,%q)，期望 (%q,%q)", c.in, r, b, c.wantRealm, c.wantBare)
		}
	}
}

// TestQoderRealmsFor 裸 qoder 按 cn→intl 回落；显式前缀钉死单区域。
func TestQoderRealmsFor(t *testing.T) {
	if got := qoderRealmsFor("qoder-cn"); len(got) != 1 || got[0] != "cn" {
		t.Fatalf("qoder-cn 应钉死 cn: %v", got)
	}
	if got := qoderRealmsFor("qoder-intl"); len(got) != 1 || got[0] != "intl" {
		t.Fatalf("qoder-intl 应钉死 intl: %v", got)
	}
	got := qoderRealmsFor("qoder")
	if len(got) != 2 || got[0] != "cn" || got[1] != "intl" {
		t.Fatalf("裸 qoder 应 cn→intl: %v", got)
	}
}
