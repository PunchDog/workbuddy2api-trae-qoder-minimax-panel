package appserver

// config_opencode_test.go opencode.api_key 热生效回归：面板保存后 livecfg 快照
// 必须同步更新（转发层下一请求即用），且不进需重启清单。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/livecfg"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/scheduler"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestSaveConfigOpenCodeKeyHotApply(t *testing.T) {
	t.Setenv("WB2A_OPENCODE_API_KEY", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"api_key":"sk-test","opencode":{"api_key":""}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	live := livecfg.New(livecfg.Snapshot{})
	p := pool.New(filepath.Join(dir, "state.json"))
	up := upstream.New()
	sch := scheduler.New(scheduler.Config{})

	restart, err := saveConfig([]byte(`{"opencode":{"api_key":"sk-oc-new"}}`), path, live, p, up, sch)
	if err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	if live.Load().OpenCodeAPIKey != "sk-oc-new" {
		t.Fatalf("热快照未更新: %q", live.Load().OpenCodeAPIKey)
	}
	for _, f := range restart {
		if strings.Contains(f, "opencode") {
			t.Fatalf("opencode.api_key 应热生效无需重启: %v", restart)
		}
	}

	// 清空 key 同样热更新（转发回退调用方 Key/env 兜底）。
	if _, err := saveConfig([]byte(`{"opencode":{"api_key":""}}`), path, live, p, up, sch); err != nil {
		t.Fatalf("二次 saveConfig: %v", err)
	}
	if live.Load().OpenCodeAPIKey != "" {
		t.Fatalf("清空后热快照未归零: %q", live.Load().OpenCodeAPIKey)
	}
}