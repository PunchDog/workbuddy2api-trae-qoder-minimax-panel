//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

/**
 * job.go —— 启动器子进程回收兜底（Windows Job Object）。
 *
 * 问题：launch() 用 exec.Command 拉起 --server 后 Release 句柄，启动器一旦
 * 被任务管理器强杀、崩溃或异常退出，服务子进程就会残留在进程列表里。
 *
 * 方案：创建 kill-on-close 的 Job Object，把服务子进程加入；启动器进程句柄
 * 关闭（任何退出方式）时由内核终止 Job 内进程。创建失败则静默降级——仍有
 * 托盘「退出」的静默停网关与 --stop 两条显式清理路径。
 */
var jobHandle windows.Handle

// setupKillOnCloseJob 创建 Job 并设置「句柄关闭即杀光子进程」。
func setupKillOnCloseJob() {
	if jobHandle != 0 {
		return
	}
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(h)
		return
	}
	jobHandle = h
}

// assignToKillJob 把服务子进程加入 Job；失败不影响服务本身。
func assignToKillJob(p *os.Process) {
	if jobHandle == 0 || p == nil {
		return
	}
	ph, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(p.Pid),
	)
	if err != nil {
		return
	}
	defer windows.CloseHandle(ph)
	_ = windows.AssignProcessToJobObject(jobHandle, ph)
}
