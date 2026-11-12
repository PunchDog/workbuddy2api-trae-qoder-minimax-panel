package ideauth

import "testing"

// TestQoderAuthKey 账号标识回落链：邮箱 → 手机号 → 用户 id。
// 国内版大量账号是手机号注册（user.email 为空），标识为空会被号池拒收，
// 这条回落链是「国内版账号能收录」的前提。
func TestQoderAuthKey(t *testing.T) {
	cases := []struct {
		name string     // 用例名
		auth *QoderAuth // 输入登录态
		want string     // 期望标识
	}{
		{"nil 登录态", nil, ""},
		{"邮箱优先", &QoderAuth{User: QoderUser{Email: "a@x.com", Phone: "13800000000", ID: "uid-1"}}, "a@x.com"},
		{"邮箱去空白", &QoderAuth{User: QoderUser{Email: "  a@x.com  "}}, "a@x.com"},
		{"无邮箱回落手机号", &QoderAuth{User: QoderUser{Phone: "18608008318", ID: "uid-1"}}, "18608008318"},
		{"手机号是 JSON 数字", &QoderAuth{User: QoderUser{Phone: float64(18608008318), ID: "uid-1"}}, "18608008318"},
		{"邮箱手机号皆无回落 uid", &QoderAuth{User: QoderUser{ID: "uid-1"}}, "uid-1"},
		{"全空", &QoderAuth{}, ""},
		{"phone 为 null", &QoderAuth{User: QoderUser{Phone: nil, ID: "uid-2"}}, "uid-2"},
	}
	for _, c := range cases {
		if got := QoderAuthKey(c.auth); got != c.want {
			t.Errorf("%s: QoderAuthKey = %q，期望 %q", c.name, got, c.want)
		}
	}
}
