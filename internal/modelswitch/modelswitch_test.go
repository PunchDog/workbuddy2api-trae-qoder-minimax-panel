package modelswitch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultOpenWhenMissing(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "model_switches.json"))
	if !s.Enabled("cn:any") {
		t.Fatal("台账缺失时应默认全开")
	}
	def, models := s.Snapshot()
	if !def || len(models) != 0 {
		t.Fatalf("空台账快照: def=%v models=%v", def, models)
	}
}

func TestSetPersistAndLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model_switches.json")
	s := New(path)
	n, err := s.Set(map[string]bool{"cn:modelA": false, "opencode:modelX": true})
	if err != nil || n != 2 {
		t.Fatalf("Set: n=%d err=%v", n, err)
	}
	if s.Enabled("cn:modelA") {
		t.Fatal("关闭项应立即生效")
	}
	if !s.Enabled("opencode:modelX") {
		t.Fatal("显式开启项应为 true")
	}
	if !s.Enabled("cn:unrecorded") {
		t.Fatal("未记录项应默认全开")
	}

	// 重启后保留：重新从磁盘加载。
	s2 := New(path)
	if s2.Enabled("cn:modelA") {
		t.Fatal("重启后关闭项应保留")
	}
	def, models := s2.Snapshot()
	if !def || len(models) != 2 {
		t.Fatalf("落盘快照: def=%v models=%v", def, models)
	}
}

func TestCorruptFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model_switches.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if !s.Enabled("anything") {
		t.Fatal("损坏台账应回退默认全开")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("原始损坏文件应保留")
	}
	matches, _ := filepath.Glob(path + ".corrupt-*")
	if len(matches) == 0 {
		t.Fatal("应保留损坏副本便于排查")
	}
}

func TestOverwriteAndEmptyIdIgnored(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "model_switches.json"))
	_, _ = s.Set(map[string]bool{"cn:a": false})
	n, err := s.Set(map[string]bool{"cn:a": true, "": false})
	if err != nil || n != 1 {
		t.Fatalf("Set: n=%d err=%v", n, err)
	}
	if !s.Enabled("cn:a") {
		t.Fatal("覆盖写入应生效")
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "model_switches.json"))
	_, _ = s.Set(map[string]bool{"cn:a": false})
	_, models := s.Snapshot()
	models["cn:a"] = true
	delete(models, "cn:a")
	if s.Enabled("cn:a") {
		t.Fatal("修改快照副本不应影响台账")
	}
}
