//go:build windows

package proc

import (
	"os/exec"
	"syscall"
	"time"
)

func setSysProcAttr(cmd *exec.Cmd) {
	// Windows 无进程组；HideWindow 避免闪控制台
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// terminateGraceful: Windows 下直接 Kill（Luanti 服务器一般无子进程），
// 进程回收由 runOnce 的 cmd.Wait() 负责。
func terminateGraceful(cmd *exec.Cmd, timeout time.Duration) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
