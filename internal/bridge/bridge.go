// Package bridge: 面板 ↔ 服务端进程的命令通道。
//
// 现代 Luanti 专用服务器不读取 stdin（dedicated_server_loop 无输入处理），
// 因此控制台命令通过随世界自动加载的 panel_bridge mod 走 HTTP 回调：
// mod 每两秒轮询面板取命令 → 以管理员身份执行（registered_chatcommands）
// → 把输出 POST 回面板。该架构与 minetest-go/mtui 的 bridge 一致。
package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

func randID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type PendingCmd struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	result chan string
	at     time.Time
}

type Bridge struct {
	mu       sync.Mutex
	pending  map[string][]*PendingCmd            // instanceID → 待轮询命令
	inflight map[string]map[string]*PendingCmd   // instanceID → cmdID → 已下发待结果
}

func New() *Bridge {
	b := &Bridge{
		pending:  map[string][]*PendingCmd{},
		inflight: map[string]map[string]*PendingCmd{},
	}
	go b.janitor()
	return b
}

// Queue 入队一条命令，返回命令 ID 与结果通道（janitor 保证通道最终会被关闭）。
func (b *Bridge) Queue(instID, text string) (string, <-chan string) {
	cmd := &PendingCmd{ID: randID(), Text: text, result: make(chan string, 1), at: time.Now()}
	b.mu.Lock()
	b.pending[instID] = append(b.pending[instID], cmd)
	b.mu.Unlock()
	return cmd.ID, cmd.result
}

// Poll 取走实例的待执行命令（bridge mod 心跳时调用），转入 inflight 等待结果。
func (b *Bridge) Poll(instID string) []*PendingCmd {
	b.mu.Lock()
	defer b.mu.Unlock()
	cmds := b.pending[instID]
	delete(b.pending, instID)
	if len(cmds) > 0 && b.inflight[instID] == nil {
		b.inflight[instID] = map[string]*PendingCmd{}
	}
	for _, c := range cmds {
		b.inflight[instID][c.ID] = c
	}
	return cmds
}

// Result 交付命令输出；返回是否成功匹配。
func (b *Bridge) Result(instID, cmdID, output string) bool {
	b.mu.Lock()
	m := b.inflight[instID]
	cmd, ok := m[cmdID]
	if ok {
		delete(m, cmdID)
	}
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case cmd.result <- output:
	default:
	}
	return true
}

// DropInstance 实例停止/删除时清空其命令，通知等待方。
func (b *Bridge) DropInstance(instID string) {
	b.mu.Lock()
	var chans []chan string
	for _, c := range b.pending[instID] {
		chans = append(chans, c.result)
	}
	delete(b.pending, instID)
	for _, c := range b.inflight[instID] {
		chans = append(chans, c.result)
	}
	delete(b.inflight, instID)
	b.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- "（实例已停止，命令未送达）":
		default:
		}
	}
}

// janitor 兜底：超过 75s 仍未返回结果的命令关闭其通道，避免 HTTP 侧永久等待。
func (b *Bridge) janitor() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for range t.C {
		b.mu.Lock()
		var chans []chan string
		for _, m := range b.inflight {
			for id, c := range m {
				if time.Since(c.at) > 75*time.Second {
					chans = append(chans, c.result)
					delete(m, id)
				}
			}
		}
		for inst, list := range b.pending {
			var keep []*PendingCmd
			for _, c := range list {
				if time.Since(c.at) > 75*time.Second {
					chans = append(chans, c.result)
				} else {
					keep = append(keep, c)
				}
			}
			if len(keep) == 0 {
				delete(b.pending, inst)
			} else {
				b.pending[inst] = keep
			}
		}
		b.mu.Unlock()
		for _, ch := range chans {
			select {
			case ch <- "（命令超时：服务端未连接桥接或未及时响应）":
			default:
			}
		}
	}
}
