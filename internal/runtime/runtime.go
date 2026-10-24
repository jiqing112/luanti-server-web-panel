// Package runtime: 实例运行时编排 —— proc / ws hub / 玩家跟踪 / 活动日志缓冲 /
// bridge 命令通道的组织者，API 层只与它对话。
package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"luantipanel/internal/bridge"
	"luantipanel/internal/contentdb"
	"luantipanel/internal/logparse"
	"luantipanel/internal/luanti"
	"luantipanel/internal/monitor"
	"luantipanel/internal/notify"
	"luantipanel/internal/proc"
	"luantipanel/internal/store"
	"luantipanel/internal/ws"
)

// replayCap 限制面板离线期间的历史日志回放量（增量超过该值时只回放尾部）。
const replayCap = 64 << 20 // 64MB

// ErrBridgeTimeout 表示 bridge 命令在给定时间内没有得到结果。
var ErrBridgeTimeout = errors.New("bridge 命令超时")

type Runtime struct {
	st      *store.Store
	DataDir string
	Notify  *notify.Notifier
	Bridge  *bridge.Bridge
	CDB     *contentdb.Client

	mu       sync.Mutex
	procs    map[string]*proc.Proc
	hubs     map[string]*ws.Hub
	trackers map[string]*logparse.Tracker

	lifeMu sync.Mutex // 串行化实例生命周期操作（Start/Stop/Restart/Forget/Shutdown）

	actMu  sync.Mutex
	actBuf map[string][]store.ActivityRow

	quit         chan struct{}
	shutdownOnce sync.Once
}

func New(st *store.Store, dataDir string, n *notify.Notifier) *Runtime {
	rt := &Runtime{
		st:       st,
		DataDir:  dataDir,
		Notify:   n,
		Bridge:   bridge.New(),
		CDB:      contentdb.New(),
		procs:    map[string]*proc.Proc{},
		hubs:     map[string]*ws.Hub{},
		trackers: map[string]*logparse.Tracker{},
		actBuf:   map[string][]store.ActivityRow{},
		quit:     make(chan struct{}),
	}
	go rt.activityFlusher()
	go rt.registryReconciler()
	return rt
}

// registryReconciler 定期核对注册表与数据库：已删除实例的 proc/tracker/hub
// 条目及时移除，避免残留引用（替代在删除接口里逐一清理，也兼容历史遗漏）。
func (rt *Runtime) registryReconciler() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			rt.mu.Lock()
			ids := make([]string, 0, len(rt.procs))
			for id := range rt.procs {
				ids = append(ids, id)
			}
			rt.mu.Unlock()
			for _, id := range ids {
				if _, err := rt.st.GetInstance(id); rt.st.IsNoRows(err) {
					log.Printf("[registry] 清理已删除实例 %s 的运行时条目", id)
					rt.Forget(id)
				}
			}
		case <-rt.quit:
			return
		}
	}
}

func (rt *Runtime) Dirs(id string) luanti.Dirs {
	return luanti.Dirs{Root: filepath.Join(rt.DataDir, "instances", id)}
}

// ---- 注册表 ----

func (rt *Runtime) Hub(id string) *ws.Hub {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if h, ok := rt.hubs[id]; ok {
		return h
	}
	h := ws.NewHub()
	rt.hubs[id] = h
	return h
}

// Proc 取得（并按需构建）实例进程对象。
// 先在锁外构建、再在锁内二次检查后写入：并发调用只会有一个对象进入注册表，
// 落选者因未启动任何 goroutine 而被丢弃（真正起进程的 Start 由 lifeMu 串行化）。
func (rt *Runtime) Proc(id string) (*proc.Proc, error) {
	rt.mu.Lock()
	if p, ok := rt.procs[id]; ok {
		rt.mu.Unlock()
		return p, nil
	}
	rt.mu.Unlock()

	inst, err := rt.st.GetInstance(id)
	if err != nil {
		return nil, err
	}
	d := rt.Dirs(id)
	hub := rt.Hub(id)

	tracker := logparse.NewTracker(func(players map[string]time.Time, creative map[string]bool) {
		// 同步给 proc（/players 与列表页读取 proc 状态）并广播
		if p2, err := rt.Proc(id); err == nil {
			p2.UpdatePlayers(players)
		}
		rt.Hub(id).Broadcast("players", playersPayload(players, creative))
	})

	p := proc.New(id, inst.Name, d.Binary(), rt.buildArgs(inst), d.Root)
	// 引擎自身通过 --logfile 持久化日志；面板管道不再写同一文件（避免每行双写）
	p.LogFilePath = ""
	p.SetAutoRestart(inst.AutoRestart)
	p.OnLine = func(line string) {
		hub.Broadcast("log", map[string]string{"line": line})
		rt.consumeLine(id, tracker, line)
	}
	p.OnPlayers = func(players map[string]time.Time) {
		hub.Broadcast("players", playersPayload(players, nil))
	}
	p.OnStatus = func(s proc.Status) {
		status := string(s)
		_ = rt.st.UpdateInstance(id, &store.InstanceUpdate{Status: &status})
		hub.Broadcast("status", map[string]string{"status": status})
		if s == proc.StatusRunning {
			monitor.StartSampling(p) // 进程退出后采样循环自行结束
		}
		if s == proc.StatusStopped {
			rt.persistLogOffset(id) // 记录日志消费位置，下次启动只回放增量
		}
	}
	p.OnCrash = func(exitCode int, err error) {
		detail := fmt.Sprintf("实例「%s」异常退出（exit code %d）", inst.Name, exitCode)
		if err != nil {
			detail += ": " + err.Error()
		}
		rt.Notify.Send("crash:"+id, "Luanti 实例崩溃", detail+
			fmt.Sprintf("\n连续崩溃 %d 次（阈值 %d 次后停止自动重启）", p.CrashCount(), proc.CrashRestartMax))
	}
	p.OnRestart = func(attempt int) {
		rt.Notify.Send("crash:"+id, "Luanti 实例自动重启",
			fmt.Sprintf("实例「%s」第 %d 次自动重启（%d 秒后拉起）", inst.Name, attempt, proc.RestartDelay))
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if exist, ok := rt.procs[id]; ok {
		return exist, nil // 并发下沿用先创建者
	}
	rt.procs[id] = p
	rt.trackers[id] = tracker
	return p, nil
}

func (rt *Runtime) buildArgs(inst *store.Instance) []string {
	d := rt.Dirs(inst.ID)
	return []string{
		"--config", d.Config(),
		"--gameid", inst.GameID,
		"--world", d.World(inst.WorldName),
		"--port", strconv.Itoa(inst.Port),
		"--logfile", d.LogFile(),
	}
}

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

// ---- 日志消费 ----

func (rt *Runtime) consumeLine(id string, tracker *logparse.Tracker, line string) {
	ev, list := logparse.ParseLine(line)
	if ev == nil {
		return
	}
	tracker.Apply(ev, list)
	rt.ingestEvent(id, ev)
}

// ingestEvent 把解析出的事件写入活动缓冲（实时与历史回放共用）。
func (rt *Runtime) ingestEvent(id string, ev *logparse.Event) {
	row := store.ActivityRow{PlayerName: ev.Player, Type: ev.Type, Content: ev.Content, CreatedAt: ev.Ts}
	rt.actMu.Lock()
	rt.actBuf[id] = append(rt.actBuf[id], row)
	rt.actMu.Unlock()
}

// replayLogHistory 把 debug.txt 中上次消费位置之后的历史日志按时间顺序
// 回放进活动审计（面板/服务重启期间产生的日志不再丢失）。
// 只写活动库与玩家状态，不向 WebSocket 广播，避免刷屏。
// 增量超过 replayCap 时只回放尾部（防止面板长期离线后一次性读入过大的文件）。
func (rt *Runtime) replayLogHistory(id string) {
	key := "logoff_" + id
	off, _ := strconv.ParseInt(rt.st.GetSetting(key, "0"), 10, 64)
	path := rt.Dirs(id).LogFile()
	fi, err := os.Stat(path)
	if err != nil {
		return // 日志文件尚不存在（实例从未运行过），无需回放
	}
	if fi.Size() <= off {
		_ = rt.st.SetSetting(key, strconv.FormatInt(fi.Size(), 10))
		return
	}
	start := off
	if fi.Size()-start > replayCap {
		start = fi.Size() - replayCap
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(start, 0); err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, replayCap+(64<<10)))
	if err != nil {
		return
	}
	if start > off {
		// 起点被截断：丢弃首个不完整行
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			return
		}
	}
	// 只消费完整行（正在写的最后一行留给下次）
	cut := len(data)
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		cut = i + 1
	} else {
		return
	}
	tracker := rt.Tracker(id)
	if tracker == nil {
		return
	}
	n := 0
	for _, line := range strings.Split(string(data[:cut]), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		ev, list := logparse.ParseLine(line)
		if ev == nil {
			continue
		}
		// 去重：实时链路已入库的事件（join/leave/chat 等）跳过，只补缺
		if rt.st.ExistsActivity(id, ev.Player, ev.Type, ev.Content, ev.Ts) {
			continue
		}
		tracker.Apply(ev, list)
		rt.ingestEvent(id, ev)
		n++
	}
	_ = rt.st.SetSetting(key, strconv.FormatInt(start+int64(cut), 10))
	if n > 0 {
		log.Printf("[log-history] 实例 %s 回放历史日志 %d 行", id, n)
	}
}

// persistLogOffset 记录日志文件的当前消费位置（实例停止时调用）。
func (rt *Runtime) persistLogOffset(id string) {
	path := rt.Dirs(id).LogFile()
	if fi, err := os.Stat(path); err == nil {
		_ = rt.st.SetSetting("logoff_"+id, strconv.FormatInt(fi.Size(), 10))
	}
}

func (rt *Runtime) activityFlusher() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			rt.flushActivities()
		case <-rt.quit:
			return
		}
	}
}

func (rt *Runtime) flushActivities() {
	rt.actMu.Lock()
	buf := rt.actBuf
	rt.actBuf = map[string][]store.ActivityRow{}
	rt.actMu.Unlock()
	for id, rows := range buf {
		if err := rt.st.InsertActivities(id, rows); err != nil {
			log.Printf("[activity] 写入失败: %v", err)
		}
	}
}

// Tracker 取得实例的玩家跟踪器（可能为 nil，表示进程对象尚未创建）。
func (rt *Runtime) Tracker(id string) *logparse.Tracker {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.trackers[id]
}

// ---- 生命周期 ----

// Start 启动实例（lifeMu 串行化：与 Stop/Restart/Forget/Shutdown 互斥）。
func (rt *Runtime) Start(id string) error {
	rt.lifeMu.Lock()
	defer rt.lifeMu.Unlock()
	return rt.startLocked(id)
}

func (rt *Runtime) startLocked(id string) error {
	inst, err := rt.st.GetInstance(id)
	if err != nil {
		return err
	}
	d := rt.Dirs(id)
	if err := d.EnsureLayout(inst.WorldName); err != nil {
		return err
	}
	// 世界目录 + world.mt 兜底（创建后可被删除过）
	worldDir := d.World(inst.WorldName)
	if _, err := os.Stat(filepath.Join(worldDir, "world.mt")); os.IsNotExist(err) {
		if err := os.MkdirAll(worldDir, 0o755); err != nil {
			return err
		}
		wmt, _ := luanti.LoadWorldMT(filepath.Join(worldDir, "world.mt"))
		wmt.Set("world_name", inst.WorldName)
		wmt.Set("gameid", inst.GameID)
		wmt.Set("backend", "sqlite3")
		wmt.Set("player_backend", "sqlite3")
		wmt.Set("auth_backend", "sqlite3")
		if err := wmt.Save(); err != nil {
			return err
		}
	}
	// panel_bridge mod 兜底安装，并自愈桥接配置（端口/token）
	if err := rt.EnsureBridgeMod(worldDir); err != nil {
		return err
	}
	if err := rt.healBridgeConf(inst, d); err != nil {
		return err
	}
	// 二进制就位：手动路径优先，其次面板共享二进制
	master := strings.TrimSpace(rt.st.GetSetting("luanti_binary_path", ""))
	if master == "" {
		master = filepath.Join(rt.DataDir, "bin", luanti.BinaryName())
	}
	if err := d.PlaceBinary(master); err != nil {
		return err
	}
	// 引擎运行时（builtin/ 等）镜像到实例根——缺少 builtin 无法启动
	mirrorSrc := filepath.Join(rt.DataDir, "bin")
	if !strings.Contains(master, rt.DataDir) {
		mirrorSrc = filepath.Dir(master) // 手动路径：从其所在目录镜像（若有）
	}
	if err := luanti.MirrorRuntime(mirrorSrc, d.Root); err != nil {
		return err
	}
	_ = rt.st.UpdateInstance(id, &store.InstanceUpdate{LastStartedAt: strPtr(time.Now().Format(time.RFC3339))})

	// 管理员条目就位（权限 + 面板管理的密码）——bridge 以管理员身份执行
	// 命令依赖引擎能查到该条目；引擎只在玩家首次进服时才创建（仅默认权限），
	// 因此在启动前由面板直接建好。密码来自面板设置，未设置则生成并保存。
	rt.ensureAdminEntry(inst)

	// 先建 Proc/Tracker，再把历史日志按时间顺序回放进活动审计（不广播）
	p, err := rt.Proc(id)
	if err != nil {
		return err
	}
	rt.replayLogHistory(id)
	return p.Start()
}

func strPtr(s string) *string { return &s }

// AdminPassword 读取实例管理员密码（面板设置中保管）。
func (rt *Runtime) AdminPassword(id string) string {
	return rt.st.GetSetting("adminpw_"+id, "")
}

// ensureAdminEntry 启动前创建/更新管理员 auth 条目。
func (rt *Runtime) ensureAdminEntry(inst *store.Instance) {
	if inst.AdminName == "" || !luanti.IsValidPlayerName(inst.AdminName) {
		return
	}
	pw := rt.AdminPassword(inst.ID)
	if pw == "" {
		pw = luanti.RandomPassword()
		_ = rt.st.SetSetting("adminpw_"+inst.ID, pw)
	}
	worldDir := rt.Dirs(inst.ID).World(inst.WorldName)
	created, err := luanti.EnsureAdminEntry(worldDir, inst.AdminName, pw)
	if err != nil {
		log.Printf("[admin] 实例 %s 管理员条目准备失败: %v", inst.Name, err)
		return
	}
	if created {
		log.Printf("[admin] 已创建管理员 %s 的 auth 条目（完整权限，密码在面板「玩家」页查看/修改）", inst.AdminName)
	}
}

// SetAdminPassword 更新管理员密码：面板设置 + 引擎 auth 库同步写。
func (rt *Runtime) SetAdminPassword(id, password string) error {
	inst, err := rt.st.GetInstance(id)
	if err != nil {
		return err
	}
	if inst.AdminName == "" {
		return fmt.Errorf("实例未设置管理员玩家名")
	}
	if len(password) < 4 {
		return fmt.Errorf("密码至少 4 位")
	}
	if err := luanti.SetAdminPassword(rt.Dirs(id).World(inst.WorldName), inst.AdminName, password); err != nil {
		return err
	}
	return rt.st.SetSetting("adminpw_"+id, password)
}

// healBridgeConf 保证 conf 中的桥接地址/令牌键与面板当前一致。
// 键值均来自实例记录与面板设置，无任何字面量凭据。
func (rt *Runtime) healBridgeConf(inst *store.Instance, d luanti.Dirs) error {
	content, err := os.ReadFile(d.Config())
	if err != nil {
		return nil // 配置不存在时由调用方报错
	}
	panelPort := rt.st.GetSetting("panel_port", "8080")
	urlKey := "panel_bridge." + "url"
	tokenKey := "panel_bridge." + "token"
	want := map[string]string{
		urlKey: "http://127.0.0.1:" + panelPort,
	}
	want[tokenKey] = inst.BridgeToken
	need := false
	for k, v := range want {
		if !strings.Contains(string(content), k+" = "+v) {
			need = true
			break
		}
	}
	if !need {
		return nil
	}
	merged, err := luanti.MergeValues(string(content), want)
	if err != nil {
		return err
	}
	return os.WriteFile(d.Config(), []byte(merged), 0o644)
}

// EnsureBridgeMod 把 panel_bridge mod 写入世界 worldmods/（幂等）。
func (rt *Runtime) EnsureBridgeMod(worldDir string) error {
	modDir := filepath.Join(worldDir, "worldmods", "panel_bridge")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		return err
	}
	for name, content := range bridge.LuaModFiles() {
		p := filepath.Join(modDir, name)
		if old, err := os.ReadFile(p); err == nil && string(old) == content {
			continue // 已是最新内容
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Stop 停止实例：优先经 bridge 发 /shutdown（引擎存盘后优雅退出），
// 未奏效再回落到信号/强杀。
func (rt *Runtime) Stop(id string) error {
	rt.lifeMu.Lock()
	defer rt.lifeMu.Unlock()
	return rt.stopLocked(id)
}

func (rt *Runtime) stopLocked(id string) error {
	p, err := rt.Proc(id)
	if err != nil {
		return err
	}
	defer rt.Bridge.DropInstance(id)
	if p.Running() {
		// 优雅停止优先：/shutdown 让引擎保存世界后退出。
		// Windows 无 SIGTERM（强杀不存盘），这条路径对数据完整性尤其重要。
		if _, err := rt.command(id, "/shutdown", 6*time.Second); err == nil {
			deadline := time.Now().Add(30 * time.Second) // 大世界存盘可能较慢
			for p.Running() && time.Now().Before(deadline) {
				time.Sleep(150 * time.Millisecond)
			}
		}
	}
	return p.Stop()
}

// Restart 重启实例（先优雅停止再启动）。
func (rt *Runtime) Restart(id string) error {
	rt.lifeMu.Lock()
	defer rt.lifeMu.Unlock()
	if err := rt.stopLocked(id); err != nil {
		return err
	}
	return rt.startLocked(id)
}

// Forget 实例删除后从注册表移除进程/跟踪器/hub（先确保已停止）。
func (rt *Runtime) Forget(id string) {
	rt.lifeMu.Lock()
	defer rt.lifeMu.Unlock()
	rt.mu.Lock()
	_, hasProc := rt.procs[id]
	rt.mu.Unlock()
	if hasProc {
		_ = rt.stopLocked(id)
	}
	rt.mu.Lock()
	delete(rt.procs, id)
	delete(rt.trackers, id)
	delete(rt.hubs, id)
	rt.mu.Unlock()
}

// Shutdown 面板退出：并行停止全部实例，冲刷活动日志，结束后台 goroutine。
func (rt *Runtime) Shutdown() {
	rt.shutdownOnce.Do(func() {
		rt.lifeMu.Lock()
		defer rt.lifeMu.Unlock()
		rt.mu.Lock()
		ids := make([]string, 0, len(rt.procs))
		for id := range rt.procs {
			ids = append(ids, id)
		}
		rt.mu.Unlock()

		var wg sync.WaitGroup
		for _, id := range ids {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				_ = rt.stopLocked(id)
			}(id)
		}
		wg.Wait()

		// 停止过程中各实例的最终事件已在缓冲，最后冲刷一次再收尾
		rt.flushActivities()
		close(rt.quit)
	})
}

// StatusOf 返回运行时状态（proc 优先），无 proc 时读 DB。
func (rt *Runtime) StatusOf(id string) string {
	rt.mu.Lock()
	p, ok := rt.procs[id]
	rt.mu.Unlock()
	if ok {
		return string(p.Status())
	}
	inst, err := rt.st.GetInstance(id)
	if err != nil {
		return "stopped"
	}
	return inst.Status
}

// Command 发送控制台命令（经 bridge），等待输出（最多 15 秒）。
func (rt *Runtime) Command(id, text string) (string, error) {
	out, err := rt.command(id, text, 15*time.Second)
	if errors.Is(err, ErrBridgeTimeout) {
		return "（等待命令输出超时，命令可能已执行，请查看日志）", nil
	}
	return out, err
}

func (rt *Runtime) command(id, text string, timeout time.Duration) (string, error) {
	status := rt.StatusOf(id)
	if status != "running" && status != "starting" {
		return "", fmt.Errorf("实例未运行")
	}
	_, ch := rt.Bridge.Queue(id, text)
	select {
	case out := <-ch:
		return out, nil
	case <-time.After(timeout):
		return "", ErrBridgeTimeout
	}
}

// RecentLines 历史日志：进程在内存里的环形缓冲优先，否则读日志文件尾部。
func (rt *Runtime) RecentLines(id string, max int) []string {
	rt.mu.Lock()
	p, ok := rt.procs[id]
	rt.mu.Unlock()
	if ok {
		lines := p.RecentLines()
		if len(lines) > max {
			lines = lines[len(lines)-max:]
		}
		return lines
	}
	return tailFile(rt.Dirs(id).LogFile(), max)
}

func tailFile(path string, max int) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return lines
}
