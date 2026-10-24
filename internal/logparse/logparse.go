// Package logparse: 从服务端日志行解析玩家进出与聊天/命令事件。
//
// 依据 Luanti 源码 (src/server.cpp) 的实际输出格式：
//   - 加入: "...: ACTION[Main]: <name> [<ip>] joins game. List of players: a b c <name>"
//   - 离开: "...: ACTION[Main]: <name> leaves game. List of players: ..."（超时为 "times out."）
//   - 聊天: "...: ACTION[Main]: CHAT: <name> message"
//
// 玩家名单以空格分隔；名单行是权威状态，可整体覆盖内存集合，避免漏读行导致漂移。
package logparse

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reTs    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}):`)
	reJoin  = regexp.MustCompile(` (\S{1,60}) \[[0-9a-fA-F.:]+\] joins game\. List of players: ?(.*)$`)
	reLeave = regexp.MustCompile(` (\S{1,60}) (?:leaves game\.|times out\.) List of players: ?(.*)$`)
	reChat  = regexp.MustCompile(` CHAT: <([^>\s]{1,60})> (.*)$`)
	rePlace = regexp.MustCompile(` (\S{1,60}) places node (\S+) at \((-?[0-9]+,-?[0-9]+,-?[0-9]+)\)`)
	reDig   = regexp.MustCompile(` (\S{1,60}) digs node (\S+) at \((-?[0-9]+,-?[0-9]+,-?[0-9]+)\)`)
)

type Event struct {
	Type    string // join | leave | chat | command | place | dig
	Player  string
	Content string
	Ts      string
}

// ParseLine 解析单行日志。第二返回值为该行携带的玩家名单（join/leave 行才有）。
func ParseLine(line string) (*Event, []string) {
	var list []string
	if m := reJoin.FindStringSubmatch(line); m != nil {
		list = splitPlayers(m[2])
		return &Event{Type: "join", Player: m[1], Ts: extractTs(line)}, list
	}
	if m := reLeave.FindStringSubmatch(line); m != nil {
		list = splitPlayers(m[2])
		return &Event{Type: "leave", Player: m[1], Ts: extractTs(line)}, list
	}
	if m := reChat.FindStringSubmatch(line); m != nil {
		ev := &Event{Type: "chat", Player: m[1], Content: m[2], Ts: extractTs(line)}
		if strings.HasPrefix(m[2], "/") {
			ev.Type = "command"
		}
		return ev, nil
	}
	// 方块操作（引擎 ACTION 行）：活动审计时间线也展示最新放置/挖掘
	if m := rePlace.FindStringSubmatch(line); m != nil {
		return &Event{Type: "place", Player: m[1], Content: m[2] + " (" + m[3] + ")", Ts: extractTs(line)}, nil
	}
	if m := reDig.FindStringSubmatch(line); m != nil {
		return &Event{Type: "dig", Player: m[1], Content: m[2] + " (" + m[3] + ")", Ts: extractTs(line)}, nil
	}
	return nil, nil
}

func extractTs(line string) string {
	if m := reTs.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

func splitPlayers(s string) []string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if isValidPlayerName(f) {
			out = append(out, f)
		}
	}
	return out
}

// isValidPlayerName 过滤非玩家名（实体、爆炸等误报不会匹配到本结构，
// 这里再做一次字符白名单防御）。
func isValidPlayerName(s string) bool {
	if s == "" || len(s) > 60 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}

// Tracker 维护一个实例的在线玩家（名字 → 加入时间）与创造模式标志。
type Tracker struct {
	mu       sync.Mutex
	players  map[string]time.Time
	creative map[string]bool
	onChange func(players map[string]time.Time, creative map[string]bool)
}

func NewTracker(onChange func(map[string]time.Time, map[string]bool)) *Tracker {
	return &Tracker{
		players:  map[string]time.Time{},
		creative: map[string]bool{},
		onChange: onChange,
	}
}

// Apply 处理日志事件，状态变化时回调。
func (t *Tracker) Apply(ev *Event, list []string) {
	if ev == nil {
		return
	}
	t.mu.Lock()
	changed := false
	switch ev.Type {
	case "join":
		if list != nil {
			// 名单是权威状态：同步集合，保留既有加入时间
			next := make(map[string]time.Time, len(list))
			for _, n := range list {
				if old, ok := t.players[n]; ok {
					next[n] = old
				} else {
					next[n] = time.Now()
					changed = true
				}
			}
			if len(next) != len(t.players) {
				changed = true
			}
			t.players = next
		} else if _, ok := t.players[ev.Player]; !ok {
			t.players[ev.Player] = time.Now()
			changed = true
		}
	case "leave":
		if list != nil {
			next := make(map[string]time.Time, len(list))
			for _, n := range list {
				if old, ok := t.players[n]; ok {
					next[n] = old
				} else {
					next[n] = time.Now()
				}
			}
			if _, was := t.players[ev.Player]; was {
				changed = true
			}
			t.players = next
		} else if _, ok := t.players[ev.Player]; ok {
			delete(t.players, ev.Player)
			changed = true
		}
	}
	snap := t.snapshotLocked()
	cre := t.creativeSnapshotLocked()
	t.mu.Unlock()
	if changed && t.onChange != nil {
		t.onChange(snap, cre)
	}
}

// Replace 用权威名单整体替换（bridge 心跳）。
func (t *Tracker) Replace(names []string) {
	t.mu.Lock()
	next := make(map[string]time.Time, len(names))
	for _, n := range names {
		if old, ok := t.players[n]; ok {
			next[n] = old
		} else {
			next[n] = time.Now()
		}
	}
	t.players = next
	snap := t.snapshotLocked()
	cre := t.creativeSnapshotLocked()
	t.mu.Unlock()
	if t.onChange != nil {
		t.onChange(snap, cre)
	}
}

func (t *Tracker) Clear() {
	t.mu.Lock()
	t.players = map[string]time.Time{}
	snap := t.snapshotLocked()
	cre := t.creativeSnapshotLocked()
	t.mu.Unlock()
	if t.onChange != nil {
		t.onChange(snap, cre)
	}
}

// SetCreative 更新创造模式标志（bridge 心跳上报），变化时回调。
// flags 为空（旧版 bridge 未上报）时忽略，不清空既有状态。
func (t *Tracker) SetCreative(flags map[string]bool) {
	if len(flags) == 0 {
		return
	}
	t.mu.Lock()
	changed := false
	for n, v := range flags {
		if _, online := t.players[n]; !online {
			continue
		}
		if t.creative[n] != v {
			t.creative[n] = v
			changed = true
		}
	}
	for n := range t.creative {
		if _, online := t.players[n]; !online {
			delete(t.creative, n)
			changed = true
		}
	}
	snap := t.snapshotLocked()
	cre := t.creativeSnapshotLocked()
	t.mu.Unlock()
	if changed && t.onChange != nil {
		t.onChange(snap, cre)
	}
}

// CreativeOf 返回玩家创造模式标志（未知为 false）。
func (t *Tracker) CreativeOf(name string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.creative[name]
}

func (t *Tracker) creativeSnapshotLocked() map[string]bool {
	out := make(map[string]bool, len(t.creative))
	for k, v := range t.creative {
		out[k] = v
	}
	return out
}

func (t *Tracker) snapshotLocked() map[string]time.Time {
	out := make(map[string]time.Time, len(t.players))
	for k, v := range t.players {
		out[k] = v
	}
	return out
}

func (t *Tracker) Snapshot() map[string]time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked()
}
