package proc

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess 既是普通测试，也作为子进程运行：
//   - 父进程用测试二进制自身启动子进程，并追加位置参数指定模式；
//   - "crash"：立即以退出码 1 结束（模拟崩溃）；
//   - "run"  ：长期存活，直到被父进程杀掉。
func TestHelperProcess(t *testing.T) {
	mode := ""
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-") {
			mode = a
		}
	}
	switch mode {
	case "crash":
		os.Exit(1)
	case "run":
		for {
			time.Sleep(time.Hour) // 存活直到被 Kill；select{} 会触发死锁检测
		}
	}
	// 普通测试运行：无模式参数，直接结束
}

func helperProc(t *testing.T, mode string, autoRestart bool, hooks func(*Proc)) *Proc {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("定位测试二进制失败: %v", err)
	}
	p := New("t", "t", exe, []string{"-test.run=TestHelperProcess", "--", mode}, ".")
	// 注意：testing 包会忽略 "--" 之后的位置参数？不会——flag 解析在第一个非 flag 参数处
	// 停止，"-test.run=TestHelperProcess -- crash" 中的 "--" 与 "crash" 都会留在 os.Args，
	// helper 取最后一个非 flag 参数（crash / run）。
	p.SetAutoRestart(autoRestart)
	if hooks != nil {
		hooks(p)
	}
	return p
}

func waitStatus(t *testing.T, p *Proc, want Status, timeout time.Duration) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last Status
	for time.Now().Before(deadline) {
		last = p.Status()
		if last == want {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待状态 %s 超时（当前 %s）", want, last)
	return last
}

// TestAutoRestart 回归：崩溃后 supervise 必须真正等待 RestartDelay 并再次拉起。
// （旧实现 runOnce 退出时关闭 stopWait，导致重启等待的 select 立即命中“停止”分支，
// 自动重启从未生效——本测试在旧实现下必然失败。）
func TestAutoRestart(t *testing.T) {
	runs := 0
	p := helperProc(t, "crash", true, func(p *Proc) {
		p.OnStatus = func(s Status) {
			if s == StatusRunning {
				runs++
			}
		}
	})
	if err := p.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer p.Stop()

	// 第一次运行 + 至少一次自动重启 → running 状态至少出现 2 次
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if runs >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if runs < 2 {
		t.Fatalf("自动重启未发生：running 状态仅出现 %d 次", runs)
	}
	if got := p.Status(); got != StatusRunning && got != StatusRestarting && got != StatusStarting {
		t.Fatalf("重启后应回到运行态，实际 %s", got)
	}
	if c := p.CrashCount(); c < 1 {
		t.Fatalf("崩溃计数未累加: %d", c)
	}
}

// TestManualStop 手动停止：长驻进程应在宽限期内退出且状态干净。
func TestManualStop(t *testing.T) {
	p := helperProc(t, "run", true, nil)
	if err := p.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	waitStatus(t, p, StatusRunning, 15*time.Second)

	done := make(chan error, 1)
	go func() { done <- p.Stop() }()
	select {
	case <-done:
	case <-time.After(StopGraceTimeout + 15*time.Second):
		t.Fatal("Stop 超时未返回")
	}
	if s := p.Status(); s != StatusStopped {
		t.Fatalf("停止后状态应为 stopped，实际 %s", s)
	}
	if p.Running() {
		t.Fatal("停止后 running 仍为 true")
	}
	// 停止后监督循环应已退出，可再次启动
	if p.Supervising() {
		t.Fatal("停止后 supervise 仍存活")
	}
}

// TestCrashWithoutRestart 未启用自动重启：崩溃一次后进入 failed。
func TestCrashWithoutRestart(t *testing.T) {
	p := helperProc(t, "crash", false, nil)
	if err := p.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer p.Stop()
	waitStatus(t, p, StatusFailed, 15*time.Second)
}

// TestStartTwiceRejected 监督循环存活期间（含重启等待窗口）二次 Start 必须被拒绝。
func TestStartTwiceRejected(t *testing.T) {
	p := helperProc(t, "run", true, nil)
	if err := p.Start(); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	defer p.Stop()
	waitStatus(t, p, StatusRunning, 15*time.Second)
	if err := p.Start(); err == nil {
		t.Fatal("运行中二次 Start 应返回错误")
	}
}
