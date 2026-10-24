package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"luantipanel/internal/auth"
	"luantipanel/internal/luanti"
)

// playersPayload 把在线玩家 map 转为 API 负载（creative 为创造模式标志，可为 nil）。
func playersPayload(players map[string]time.Time, creative map[string]bool) map[string]any {
	list := make([]map[string]any, 0, len(players))
	now := time.Now()
	for name, since := range players {
		list = append(list, map[string]any{
			"name": name, "since": since.Unix(), "seconds": int(now.Sub(since).Seconds()),
			"creative": creative[name],
		})
	}
	return map[string]any{"list": list}
}

// ---- 在线玩家 ----

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	p, err := s.RT.Proc(i.ID)
	if err != nil {
		writeJSON(w, 200, map[string]any{"list": []any{}})
		return
	}
	snap := p.Snapshot()
	tr := s.RT.Tracker(i.ID)
	list := make([]map[string]any, 0, len(snap.Players))
	now := time.Now()
	for name, since := range snap.Players {
		list = append(list, map[string]any{
			"name": name, "since": since.Unix(), "seconds": int(now.Sub(since).Seconds()),
			"creative": tr != nil && tr.CreativeOf(name),
		})
	}
	sort.Slice(list, func(a, b int) bool { return list[a]["name"].(string) < list[b]["name"].(string) })
	writeJSON(w, 200, map[string]any{"list": list})
}

// ---- 活动日志 ----

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	q := r.URL.Query()
	page := queryInt(r, "page", 1)
	size := queryInt(r, "page_size", 50)
	if size > 200 {
		size = 200
	}
	if page < 1 {
		page = 1
	}
	rows, total, err := s.st.ListActivity(i.ID,
		q.Get("player"), q.Get("type"), q.Get("from"), q.Get("to"),
		size, (page-1)*size)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows, "total": total, "page": page, "page_size": size})
}

// ---- 方块操作日志（依赖 blockwatcher mod）----

type BlockEntry struct {
	Player string `json:"player"`
	Action string `json:"action"`
	Node   string `json:"node"`
	Pos    string `json:"pos"`
	Raw    string `json:"raw"`
	Time   string `json:"time"`
}

var (
	reBWKey = regexp.MustCompile(`(\w+)\s*=\s*"([^"]*)"`)
	reBWPos = regexp.MustCompile(`pos\s*=\s*\{([^}]*)\}`)
	reBWDig = regexp.MustCompile(`\b(dig|dug|remove|placed|place|build)\b`)
)

// handleBlockLog 读取世界目录中方块审计数据。
// 支持两种来源：
//  1. blockwatch mod 的 blockwatch_data.json（minetest.deserialize 格式，按坐标键控）
//  2. blockloader/blockwatcher 目录的每日平铺 txt（宽松解析）
func (s *Server) handleBlockLog(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	q := r.URL.Query()
	worldDir := s.RT.Dirs(i.ID).World(i.WorldName)

	// 来源 1：blockwatch JSON 数据库
	jpath := filepath.Join(worldDir, "blockwatch_data.json")
	if b, err := os.ReadFile(jpath); err == nil && len(b) > 0 {
		playerFilter := strings.ToLower(q.Get("player"))
		items := parseBlockwatchJSON(string(b), playerFilter)
		writeJSON(w, 200, map[string]any{
			"available": true, "mode": "blockwatch",
			"dates": []string{}, "date": "",
			"items": items,
		})
		return
	}

	// 来源 2：每日平铺 txt
	var logDirs []string
	for _, name := range []string{"blockloader", "blockwatcher", "blockwatch"} {
		if fi, err := os.Stat(filepath.Join(worldDir, name)); err == nil && fi.IsDir() {
			logDirs = append(logDirs, filepath.Join(worldDir, name))
		}
	}
	if len(logDirs) == 0 {
		writeJSON(w, 200, map[string]any{
			"available": false,
			"dates":     []string{},
			"items":     []any{},
			"hint":      "未检测到 blockwatcher 数据目录。该功能依赖 blockwatcher Mod，请先在「Mod 管理」安装（审计推荐）",
		})
		return
	}

	dateFiles := map[string]string{}
	for _, dir := range logDirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
				continue
			}
			dateFiles[strings.TrimSuffix(e.Name(), ".txt")] = filepath.Join(dir, e.Name())
		}
	}
	dates := make([]string, 0, len(dateFiles))
	for d := range dateFiles {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	date := q.Get("date")
	if date == "" && len(dates) > 0 {
		date = dates[len(dates)-1]
	}
	playerFilter := strings.ToLower(q.Get("player"))

	items := []BlockEntry{}
	if path, ok := dateFiles[date]; ok {
		b, err := os.ReadFile(path)
		if err == nil {
			items = parseBlockLog(string(b), playerFilter)
		}
	}
	writeJSON(w, 200, map[string]any{"available": true, "mode": "txt", "dates": dates, "date": date, "items": items})
}

// blockwatch JSON 解析（minetest.deserialize 格式）：
//
//	local _={};_[1]="2026-...";_[6]="event_type";...
//	return {["(x,y,z)"]={{[_[8]]=_[2],[_[6]]="break",entity="nangua1",[_[7]]=_[3]}}, ...}
//
// 结构：外层表 → 位置键 ["(x,y,z)"] → 事件列表 → 事件对象（扁平键值对，
// 键/值可能为 _[N] 字符串引用）。事件对象在第 3 层大括号，闭合时解析。
var (
	reBWRef   = regexp.MustCompile(`_\[(\d+)\]\s*=\s*"((?:[^"\\]|\\.)*)"`)
	reBWRturn = regexp.MustCompile(`return\s*\{`)
	reBWKV    = regexp.MustCompile(`(?:\[(_\[\d+\]|"[^"]+")\]|([A-Za-z_][A-Za-z0-9_]*))\s*=\s*("(?:[^"\\]|\\.)*"|_\[\d+\])`)
)

// normRef 归一化 _[N] / _N 两种引用形式并查表；非引用返回原值。
func normRef(tok string) string {
	if strings.HasPrefix(tok, "_[") && strings.HasSuffix(tok, "]") {
		tok = "_" + tok[2:len(tok)-1]
	}
	return tok
}

func parseBlockwatchJSON(content, playerFilter string) []BlockEntry {
	out := []BlockEntry{}

	strs := map[string]string{} // "_[N]" -> 原始字符串
	for _, m := range reBWRef.FindAllStringSubmatch(content, -1) {
		strs["_"+m[1]] = strings.ReplaceAll(strings.ReplaceAll(m[2], `\"`, `"`), `\\`, `\`)
	}
	// 归一化 _[N]/_N 引用并查表；非引用原样返回
	resolveRef := func(tok string) string {
		if strings.HasPrefix(tok, "_[") && strings.HasSuffix(tok, "]") {
			tok = "_" + tok[2:len(tok)-1]
		}
		if v, ok := strs[tok]; ok {
			return v
		}
		return tok
	}

	loc := reBWRturn.FindStringIndex(content)
	if loc == nil {
		return out
	}
	body := content[loc[1]-1:] // 含外层 {，深度基准正确

	depth := 0
	inStr, esc := false, false
	pos := ""
	var posKey strings.Builder
	var cur strings.Builder

	flush := func() {
		raw := cur.String()
		cur.Reset()
		e := BlockEntry{Pos: pos}
		for _, km := range reBWKV.FindAllStringSubmatch(raw, -1) {
			key := km[1]
			if key == "" {
				key = km[2]
			}
			key = resolveRef(key)
			val := resolveRef(km[3])
			// 直接字符串值带引号：剥掉两侧引号并还原转义
			if strings.HasPrefix(km[3], `"`) {
				val = strings.Trim(val, `"`)
				val = strings.ReplaceAll(strings.ReplaceAll(val, `\"`, `"`), `\\`, `\`)
			}
			switch key {
			case "timestamp", "time":
				e.Time = val
			case "event_type", "action", "act":
				e.Action = normalizeBWAction(val)
			case "entity", "player", "owner", "name", "actor", "username":
				if e.Player == "" {
					e.Player = val
				}
			case "node_name", "node", "block":
				if e.Node == "" {
					e.Node = val
				}
			}
		}
		if e.Player == "" {
			e.Player = "Unknown" // 无归因事件（如自然折断的花草）
		}
		if playerFilter != "" && !strings.Contains(strings.ToLower(e.Player), strings.ToLower(playerFilter)) {
			return
		}
		out = append(out, e)
	}

	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inStr {
			if esc {
				if depth >= 3 {
					cur.WriteByte(ch)
				}
				esc = false
				continue
			}
			if ch == '\\' {
				if depth >= 3 {
					cur.WriteByte(ch)
				}
				esc = true
				continue
			}
			if ch == '"' {
				if depth >= 3 {
					cur.WriteByte(ch) // 保留值的引号，键值对正则依赖它
				}
				inStr = false
				if depth <= 1 {
					pos = posKey.String()
					posKey.Reset()
				}
				continue
			}
			if depth >= 3 {
				cur.WriteByte(ch)
			} else if depth == 1 {
				posKey.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '"':
			if depth >= 3 {
				cur.WriteByte('"')
			}
			inStr = true
			if depth <= 1 {
				posKey.Reset()
			}
		case '{':
			depth++
			if depth == 3 {
				cur.Reset()
			}
			if depth > 3 {
				cur.WriteByte(ch)
			}
		case '}':
			if depth == 3 {
				cur.WriteByte(ch)
				flush()
			} else if depth > 3 {
				cur.WriteByte(ch)
			}
			depth--
		default:
			if depth >= 3 {
				cur.WriteByte(ch)
			} else if depth == 1 {
				posKey.WriteByte(ch)
			}
		}
	}
	// 事件在文件中按坐标分组存储，文件顺序 ≠ 时间顺序：
	// 先整体反转（同秒内后写入的在前），再按时间戳稳定倒序，保证最新在前。
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	if len(out) > 2000 {
		out = out[:2000]
	}
	return out
}

func normalizeBWAction(v string) string {
	switch strings.ToLower(v) {
	case "break", "dig", "dug", "remove":
		return "dig"
	case "place", "placed", "build", "put":
		return "place"
	}
	return strings.ToLower(v)
}

// parseBlockLog 宽松解析：每行尝试提取 player/action/node/pos 字段。
func parseBlockLog(content, playerFilter string) []BlockEntry {
	out := []BlockEntry{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		e := BlockEntry{Raw: line}
		for _, m := range reBWKey.FindAllStringSubmatch(line, -1) {
			k, v := strings.ToLower(m[1]), m[2]
			switch k {
			case "player", "owner", "name", "actor", "username":
				if e.Player == "" {
					e.Player = v
				}
			case "node", "nodename", "block":
				if e.Node == "" {
					e.Node = v
				}
			case "action", "act", "type", "event":
				if e.Action == "" {
					e.Action = v
				}
			}
		}
		if m := reBWPos.FindStringSubmatch(line); m != nil {
			e.Pos = strings.TrimSpace(m[1])
		}
		if e.Action == "" {
			if m := reBWDig.FindString(line); m != "" {
				e.Action = m
			}
		}
		if strings.EqualFold(e.Action, "dig") || strings.EqualFold(e.Action, "dug") {
			e.Action = "dig"
		} else if strings.EqualFold(e.Action, "place") || strings.EqualFold(e.Action, "placed") || strings.EqualFold(e.Action, "build") {
			e.Action = "place"
		}
		if playerFilter != "" && !strings.Contains(strings.ToLower(e.Player), playerFilter) {
			continue
		}
		if e.Player == "" && playerFilter == "" {
			// 完全解析不出的行也展示原始内容，便于排查格式
			e.Player = "（未知）"
		}
		out = append(out, e)
		if len(out) >= 2000 {
			break
		}
	}
	return out
}

// ---- 管理员账户 ----

// handleAdminGet 管理员账户状态与面板保管的密码（玩家页展示/修改用）。
func (s *Server) handleAdminGet(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	worldDir := s.RT.Dirs(i.ID).World(i.WorldName)
	writeJSON(w, 200, map[string]any{
		"name":     i.AdminName,
		"password": s.RT.AdminPassword(i.ID),
		"status":   luanti.AdminStatus(worldDir, i.AdminName),
	})
}

// handleAdminPassword 设置管理员密码（面板设置 + 引擎 auth 库同步写）。
func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Password string `json:"password"`
	}
	if err := readBody(r, &body); err != nil || len(body.Password) < 4 {
		writeErr(w, 400, "密码至少 4 位")
		return
	}
	if err := s.RT.SetAdminPassword(i.ID, body.Password); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "admin.password", i.ID, "设置管理员密码")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- 控制台命令 ----

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Text string `json:"text"`
	}
	if err := readBody(r, &body); err != nil || strings.TrimSpace(body.Text) == "" {
		writeErr(w, 400, "缺少命令内容")
		return
	}
	text := strings.TrimSpace(body.Text)
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "command", i.ID, text)

	out, err := s.RT.Command(i.ID, text)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	// 输出也作为日志广播（前端控制台可见）
	s.RT.Hub(i.ID).Broadcast("cmdout", map[string]any{
		"cmd": text, "output": strings.Split(strings.TrimRight(out, "\n"), "\n"),
	})
	writeJSON(w, 200, map[string]any{"ok": true, "output": out})
}

// ---- WebSocket 日志流 ----

func (s *Server) handleInstanceWS(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	hub := s.RT.Hub(i.ID)

	authOK := func() bool { return auth.CurrentUser(r) != nil }
	onCommand := func(text string) {
		u := auth.CurrentUser(r)
		if u == nil {
			return
		}
		s.st.Audit(u.Username, "command.ws", i.ID, text)
		out, err := s.RT.Command(i.ID, text)
		if err != nil {
			hub.Broadcast("cmdout", map[string]any{"cmd": text, "output": []string{"错误: " + err.Error()}})
			return
		}
		hub.Broadcast("cmdout", map[string]any{
			"cmd": text, "output": strings.Split(strings.TrimRight(out, "\n"), "\n"),
		})
	}
	onHello := func(send func(string, any)) {
		snapshot := map[string]any{
			"status": s.RT.StatusOf(i.ID),
			"recent": s.RT.RecentLines(i.ID, 400),
		}
		if p, err := s.RT.Proc(i.ID); err == nil {
			snap := p.Snapshot()
			snapshot["metrics"] = snap.Metrics
			snapshot["uptime_s"] = int(snap.Uptime.Seconds())
			var creative map[string]bool
			if tr := s.RT.Tracker(i.ID); tr != nil {
				creative = map[string]bool{}
				for name := range snap.Players {
					creative[name] = tr.CreativeOf(name)
				}
			}
			snapshot["players"] = playersPayload(snap.Players, creative)
			snapshot["pid"] = snap.PID
		}
		send("hello", snapshot)
	}
	hub.Serve(w, r, authOK, onCommand, onHello, func() {})
}

// ---- bridge（服务端 mod 回调）----

func (s *Server) handleBridgePoll(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Bridge-Token")
	if token == "" {
		writeErr(w, 401, "缺少 token")
		return
	}
	i, err := s.st.GetInstanceByBridgeToken(token)
	if err != nil {
		writeErr(w, 401, "token 无效")
		return
	}
	var body struct {
		Players []json.RawMessage `json:"players"`
		Time    int64             `json:"time"`
	}
	_ = readBody(r, &body)
	// 兼容两种上报：旧版 bridge 为 ["name"]，新版为 [{"name":"x","creative":false}]
	names := make([]string, 0, len(body.Players))
	creative := map[string]bool{}
	for _, raw := range body.Players {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil && str != "" {
			names = append(names, str)
			continue
		}
		var obj struct {
			Name     string `json:"name"`
			Creative bool   `json:"creative"`
		}
		if err := json.Unmarshal(raw, &obj); err == nil && obj.Name != "" {
			names = append(names, obj.Name)
			creative[obj.Name] = obj.Creative
		}
	}

	// 权威玩家名单
	if p, err := s.RT.Proc(i.ID); err == nil {
		tracker := s.RT.Tracker(i.ID)
		if tracker != nil {
			tracker.Replace(names)
			tracker.SetCreative(creative)
		} else {
			p.SetPlayers(playersMap(names))
		}
	}

	cmds := s.RT.Bridge.Poll(i.ID)
	out := make([]map[string]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, map[string]string{"id": c.ID, "text": c.Text})
	}
	writeJSON(w, 200, map[string]any{"commands": out})
}

func playersMap(names []string) map[string]time.Time {
	m := map[string]time.Time{}
	now := time.Now()
	for _, n := range names {
		m[n] = now
	}
	return m
}

func (s *Server) handleBridgeResult(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-Bridge-Token")
	i, err := s.st.GetInstanceByBridgeToken(token)
	if err != nil {
		writeErr(w, 401, "token 无效")
		return
	}
	var body struct {
		ID     string `json:"id"`
		Output string `json:"output"`
	}
	if err := readBody(r, &body); err != nil || body.ID == "" {
		writeErr(w, 400, "缺少 id")
		return
	}
	s.RT.Bridge.Result(i.ID, body.ID, body.Output)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
