// Package proc: Luanti 子进程生命周期管理。
//
// 关键点：
//   - stdout/stderr 各由一个 goroutine 用自管 os.Pipe 读取（不用 Scanner，
//     避免超长行阻塞；也不用 StdoutPipe——Wait 会提前关闭管道截断尾部日志）；
//   - Linux 下以独立进程组启动，停止时先 SIGTERM 进程组（优雅保存退出），
//     超时后 SIGKILL；Windows 下直接 Kill（面板层会先经 bridge 发 /shutdown 优雅存盘）；
//   - 异常退出时按 auto_restart 决定是否拉起，连续崩溃超阈值则停止并告警。
//
// 通道所有权（防止自动重启失效/双重监督）：
//   - stopAll：Start() 创建，仅在 Stop() 中关闭一次，用于中止监督循环
//     （含崩溃后的重启等待）；
//   - done：supervise 退出时关闭，Stop 用它等待监督循环收尾；
//   - supervise 永不关闭 stopAll，进程退出也不再触碰任何通道。
package proc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	RecentLinesCap   = 800
	CrashRestartMax  = 5               // 连续崩溃次数上限，超过则停止拉起
	StableRunReset   = 2 * time.Minute // 连续运行超过该时长后重置崩溃计数
	RestartDelay     = 5 * time.Second
	StopGraceTimeout = 20 * time.Second
)

type Status string

const (
	StatusStopped    Status = "stopped"
	StatusStarting   Status = "starting"
	StatusRunning    Status = "running"
	StatusStopping   Status = "stopping"
	StatusRestarting Status = "restarting"
	StatusFailed     Status = "failed"
)

// Metrics 由 monitor 包填充，通过 SetMetrics 写回。
type Metrics struct {
	CPUPercent float64 `json:"cpu"`
	MemMB      float64 `json:"mem_mb"`
	UptimeS    float64 `json:"uptime_s"`
}

type Proc struct {
	ID   string
	Name string // 展示名，用于日志

	BinPath     string
	Args        []string
	Dir         string
	LogFilePath string // 面板侧日志副本（由读取循环写入，跨面板重启保留历史）

	autoRestart atomic.Bool
	sampling    atomic.Bool // 采样循环占用标志（防止叠加多个采样 goroutine）

	OnLine    func(line string)                  // 每行日志（已去 \r）
	OnPlayers func(players map[string]time.Time) // 在线玩家集合变化
	OnStatus  func(status Status)                // 状态变化
	OnCrash   func(exitCode int, err error)      // 异常退出（未启用自动重启或已达上限时也会调用）
	OnRestart func(attempt int)                  // 即将自动重启

	mu                 sync.Mutex
	cmd                *exec.Cmd
	cancel             context.CancelFunc
	running            bool // 当前子进程存活（runOnce 期间为 true）
	supervising        bool // 监督循环存活（Start 到 supervise 返回之间为 true）
	manualStop         atomic.Bool
	consecutiveCrashes int
	startedAt          time.Time
	status             Status
	ring               []string
	players            map[string]time.Time
	metrics            Metrics
	logFile            *os.File

	stopAll chan struct{} // Stop() 关闭：中止监督循环与重启等待
	done    chan struct{} // supervise 退出时关闭：Stop 等待收尾
}

func New(id, name, binPath string, args []string, dir string) *Proc {
	p := &Proc{
		ID:      id,
		Name:    name,
		BinPath: binPath,
		Args:    args,
		Dir:     dir,
		status:  StatusStopped,
		players: map[string]time.Time{},
	}
	p.ring = make([]string, 0, 64)
	return p
}

func (p *Proc) SetAutoRestart(v bool) { p.autoRestart.Store(v) }

// TryBeginSampling 抢占采样权（已被占用返回 false），防止叠加采样循环。
func (p *Proc) TryBeginSampling() bool { return p.sampling.CompareAndSwap(false, true) }

// EndSampling 释放采样权（进程退出/循环结束时调用）。
func (p *Proc) EndSampling() { p.sampling.Store(false) }

func (p *Proc) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *Proc) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// Supervising 返回监督循环是否存活（含崩溃后的重启等待窗口）。
func (p *Proc) Supervising() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.supervising
}

func (p *Proc) setStatus(s Status) {
	p.mu.Lock()
	changed := p.status != s
	p.status = s
	p.mu.Unlock()
	if changed && p.OnStatus != nil {
		p.OnStatus(s)
	}
}

// ---- 启动 ----

func (p *Proc) Start() error {
	p.mu.Lock()
	if p.supervising {
		p.mu.Unlock()
		return fmt.Errorf("实例已在运行或重启中")
	}
	if _, err := os.Stat(p.BinPath); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("找不到服务端二进制 %s：请先在设置页下载或指定路径", p.BinPath)
	}
	p.manualStop.Store(false)
	p.stopAll = make(chan struct{})
	p.done = make(chan struct{})
	p.supervising = true
	p.mu.Unlock()

	go p.supervise()
	return nil
}

func (p *Proc) supervise() {
	p.mu.Lock()
	stopAll := p.stopAll
	done := p.done
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.supervising = false
		p.mu.Unlock()
		close(done)
	}()

	for {
		exitCode, waitErr, crashed := p.runOnce()

		p.mu.Lock()
		stable := time.Since(p.startedAt) > StableRunReset
		if stable {
			p.consecutiveCrashes = 0
		}
		p.mu.Unlock()

		if !crashed { // 干净退出（shutdown / SIGTERM / 手动停止）
			p.setStatus(StatusStopped)
			return
		}

		// 崩溃
		if p.OnCrash != nil {
			p.OnCrash(exitCode, waitErr)
		}
		p.mu.Lock()
		p.consecutiveCrashes++
		crashes := p.consecutiveCrashes
		p.mu.Unlock()

		if p.manualStop.Load() {
			p.setStatus(StatusStopped)
			return
		}
		if !p.autoRestart.Load() {
			p.setStatus(StatusFailed)
			return
		}
		if crashes >= CrashRestartMax {
			p.setStatus(StatusFailed)
			return
		}
		p.setStatus(StatusRestarting)
		if p.OnRestart != nil {
			p.OnRestart(crashes)
		}
		select {
		case <-time.After(RestartDelay):
		case <-stopAll:
			p.setStatus(StatusStopped)
			return
		}
	}
}

// runOnce 启动一次进程并阻塞到退出。返回 (exitCode, err, 是否异常退出)。
func (p *Proc) runOnce() (int, error, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, p.BinPath, p.Args...)
	cmd.Dir = p.Dir
	cmd.Env = os.Environ()
	setSysProcAttr(cmd)

	// 自管 os.Pipe：os/exec 的 StdoutPipe 会在 Wait 时关闭读端，
	// 可能截断进程退出前的尾部日志（如“保存世界/正在关闭”）。
	// 手动管道在父进程关闭写端、子进程退出后由读 goroutine 自然排空到 EOF。
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		cancel()
		return -1, err, true
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		cancel()
		return -1, err, true
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	p.mu.Lock()
	p.cmd = cmd
	p.cancel = cancel
	p.startedAt = time.Now()
	p.running = true
	p.mu.Unlock()
	p.setStatus(StatusStarting)

	if err := cmd.Start(); err != nil {
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		p.mu.Lock()
		p.running = false
		p.cmd = nil
		p.mu.Unlock()
		cancel()
		return -1, err, true
	}
	// 父进程关闭写端副本：子进程退出后读端自然 EOF
	stdoutW.Close()
	stderrW.Close()
	p.setStatus(StatusRunning)

	// 打开面板侧日志副本（追加）
	p.mu.Lock()
	if p.LogFilePath != "" {
		if f, err := os.OpenFile(p.LogFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			p.logFile = f
		}
	}
	p.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.pump(stdoutR) }()
	go func() { defer wg.Done(); p.pump(stderrR) }()

	// 进程退出；读端仍在排空缓冲，随后 wg.Wait 确保尾部日志完整
	waitErr := cmd.Wait()
	wg.Wait()
	stdoutR.Close()
	stderrR.Close()
	cancel()

	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	p.mu.Lock()
	p.running = false
	p.cmd = nil
	if p.logFile != nil {
		_ = p.logFile.Close()
		p.logFile = nil
	}
	p.players = map[string]time.Time{}
	players := p.players
	p.mu.Unlock()
	if p.OnPlayers != nil {
		p.OnPlayers(players) // 通知清空
	}
	p.EndSampling() // 本次运行的采样循环随之结束，下次启动可重新抢占

	manual := p.manualStop.Load()
	crashed := waitErr != nil && !manual
	return exitCode, waitErr, crashed
}

// pump 用 Read 循环读取管道并按行分发（对行长无限制）。
func (p *Proc) pump(r *os.File) {
	br := bufio.NewReaderSize(r, 32*1024)
	buf := make([]byte, 32*1024)
	var pending []byte
	for {
		n, err := br.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			for {
				idx := indexByte(pending, '\n')
				if idx < 0 {
					break
				}
				line := strings.TrimRight(string(pending[:idx]), "\r")
				pending = pending[idx+1:]
				p.handleLine(line)
			}
			if len(pending) > 512*1024 { // 防御：无换行的超长输出
				p.handleLine(string(pending))
				pending = pending[:0]
			}
		}
		if err != nil {
			if len(pending) > 0 {
				p.handleLine(strings.TrimRight(string(pending), "\r"))
			}
			return
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

const maxLineLen = 8000

func (p *Proc) handleLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(line) > maxLineLen {
		line = line[:maxLineLen] + " …"
	}
	p.mu.Lock()
	p.ring = append(p.ring, line)
	if len(p.ring) > RecentLinesCap {
		p.ring = p.ring[len(p.ring)-RecentLinesCap:]
	}
	lf := p.logFile
	p.mu.Unlock()
	if lf != nil {
		_, _ = lf.WriteString(line + "\n")
	}
	if p.OnLine != nil {
		p.OnLine(line)
	}
}

// ---- 停止 ----

func (p *Proc) Stop() error {
	p.mu.Lock()
	if !p.supervising {
		p.mu.Unlock()
		return nil
	}
	p.manualStop.Store(true)
	cmd := p.cmd
	stopAll := p.stopAll
	done := p.done
	p.mu.Unlock()
	p.setStatus(StatusStopping)

	if cmd != nil && cmd.Process != nil {
		terminateGraceful(cmd, StopGraceTimeout)
	}
	// 中止重启等待（stopAll 只在此处关闭一次；监督循环已退出时无害）
	select {
	case <-stopAll:
	default:
		close(stopAll)
	}

	// 等待 supervise 收尾
	select {
	case <-done:
	case <-time.After(StopGraceTimeout + 10*time.Second):
	}
	p.setStatus(StatusStopped)
	return nil
}

// ---- 状态快照 ----

type Snapshot struct {
	Status  Status
	Uptime  time.Duration
	Players map[string]time.Time
	Metrics Metrics
	PID     int
}

func (p *Proc) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := Snapshot{Status: p.status, Metrics: p.metrics, Players: map[string]time.Time{}}
	if p.running {
		s.Uptime = time.Since(p.startedAt)
		if p.cmd != nil && p.cmd.Process != nil {
			s.PID = p.cmd.Process.Pid
		}
	}
	for k, v := range p.players {
		s.Players[k] = v
	}
	return s
}

func (p *Proc) SetMetrics(m Metrics) {
	p.mu.Lock()
	p.metrics = m
	p.mu.Unlock()
}

func (p *Proc) SetPlayers(players map[string]time.Time) {
	p.mu.Lock()
	p.players = players
	p.mu.Unlock()
	if p.OnPlayers != nil {
		p.OnPlayers(players)
	}
}

// UpdatePlayers 静默更新玩家集合（不触发 OnPlayers 广播）。
func (p *Proc) UpdatePlayers(players map[string]time.Time) {
	p.mu.Lock()
	p.players = players
	p.mu.Unlock()
}

func (p *Proc) RecentLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.ring))
	copy(out, p.ring)
	return out
}

func (p *Proc) CrashCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consecutiveCrashes
}
