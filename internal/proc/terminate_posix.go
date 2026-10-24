//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
	"time"
)

// setSysProcAttr 让子进程拥有独立进程组，便于整组信号。
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateGraceful: SIGTERM 进程组（Luanti 优雅保存退出），
// 以 signal 0 探测退出，超时后 SIGKILL。进程回收由 runOnce 的 Wait 负责。
func terminateGraceful(cmd *exec.Cmd, timeout time.Duration) {
	if cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pgid, syscall.Signal(0)); err != nil {
			return // 进程组已退出
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = syscall.Kill(pgid, syscall.SIGKILL)
}
