//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestKillOnCloseJob 验证 kill-on-close 语义：关闭 Job 句柄后，加入 Job 的
// 子进程必须被内核终止，而不是残留。这是「启动器被强杀后 --server 残留」的
// 直接回归守卫。
func TestKillOnCloseJob(t *testing.T) {
	setupKillOnCloseJob()
	if jobHandle == 0 {
		t.Skip("Job Object 不可用（受限环境），跳过")
	}

	cmd := exec.Command("cmd", "/c", "ping -n 60 127.0.0.1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动探针子进程失败: %v", err)
	}
	assignToKillJob(cmd.Process)

	// 模拟启动器进程结束：关闭最后一个 Job 句柄。
	_ = windows.CloseHandle(jobHandle)
	jobHandle = 0

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
		// 子进程已被内核终止。
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("关闭 Job 句柄后子进程仍在运行，kill-on-close 未生效")
	}
}
