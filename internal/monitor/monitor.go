// Package monitor: 基于 gopsutil 的进程资源采样（CPU / 内存）。
package monitor

import (
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"luantipanel/internal/proc"
)

// StartSampling 为运行中的实例启动采样循环，直到进程退出。
// 内部用进程的采样占用标志去重：同一进程同时只会有一个采样循环
// （状态回调在快速重启场景下可能多次触发，避免叠加 goroutine）。
func StartSampling(p *proc.Proc) {
	if !p.TryBeginSampling() {
		return // 已有采样循环在跑
	}
	go func() {
		defer p.EndSampling()
		var pp *process.Process
		for {
			snap := p.Snapshot()
			if snap.Status != proc.StatusRunning && snap.Status != proc.StatusStarting {
				return
			}
			if snap.PID > 0 {
				if pp == nil || int(pp.Pid) != snap.PID {
					pp, _ = process.NewProcess(int32(snap.PID))
				}
			}
			if pp != nil {
				cpu, _ := pp.CPUPercent() // 两次采样间隔内的平均 CPU%
				var memMB float64
				if mi, err := pp.MemoryInfo(); err == nil {
					memMB = float64(mi.RSS) / (1024 * 1024)
				}
				m := proc.Metrics{CPUPercent: round1(cpu), MemMB: round1(memMB), UptimeS: snap.Uptime.Seconds()}
				p.SetMetrics(m)
			}
			time.Sleep(3 * time.Second)
		}
	}()
}

func round1(v float64) float64 {
	if v < 0 {
		return 0
	}
	return float64(int(v*10+0.5)) / 10
}
