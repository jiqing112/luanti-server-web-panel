package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"luantipanel/internal/auth"
	"luantipanel/internal/luanti"
	"luantipanel/internal/store"
)

type ctxKey int

const ctxKeyInstance ctxKey = 2

// instanceCtx 把实例加载进 context（存在性校验）。
func (s *Server) instanceCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := pathParam(r, "id")
		instance, err := s.st.GetInstance(id)
		if err != nil {
			writeErr(w, http.StatusNotFound, "实例不存在")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyInstance, instance)))
	})
}

func inst(r *http.Request) *store.Instance {
	return r.Context().Value(ctxKeyInstance).(*store.Instance)
}

// ---- 实例列表 / 详情 ----

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	insts, err := s.st.ListInstances()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(insts))
	for _, i := range insts {
		out = append(out, s.instanceView(i))
	}
	writeJSON(w, 200, out)
}

// handleNextPort 建议一个未被实例占用且系统可绑定的 UDP 端口。
func (s *Server) handleNextPort(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]int{"port": s.nextFreePort()})
}

func (s *Server) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.instanceView(inst(r)))
}

// instanceView 合并 DB 状态与运行时状态/指标/在线玩家。
func (s *Server) instanceView(i *store.Instance) map[string]any {
	view := map[string]any{
		"id": i.ID, "name": i.Name, "port": i.Port,
		"gameid": i.GameID, "worldname": i.WorldName, "admin_name": i.AdminName,
		"auto_restart":       i.AutoRestart,
		"backup_interval_hours": i.BackupIntervalHours,
		"backup_keep_count":  i.BackupKeepCount,
		"created_at":         i.CreatedAt,
		"last_exit_code":     i.LastExitCode,
		"last_started_at":    i.LastStartedAt,
		"status":             s.RT.StatusOf(i.ID),
	}
	// 世界数据库（map.sqlite）大小与最近写入时间
	if st, err := os.Stat(filepath.Join(s.RT.Dirs(i.ID).World(i.WorldName), "map.sqlite")); err == nil {
		view["world_db_size"] = st.Size()
		view["world_db_modified"] = st.ModTime().Format(time.RFC3339)
	}
	rtStatus := s.RT.StatusOf(i.ID)
	if rtStatus == "running" || rtStatus == "starting" || rtStatus == "restarting" {
		if p, err := s.RT.Proc(i.ID); err == nil {
			snap := p.Snapshot()
			view["metrics"] = snap.Metrics
			view["pid"] = snap.PID
			view["uptime_s"] = int(snap.Uptime.Seconds())
			view["players_online"] = len(snap.Players)
		}
	}
	return view
}

// ---- 创建 ----

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	if auth.CurrentUser(r).Role != "admin" {
		writeErr(w, http.StatusForbidden, "需要管理员权限")
		return
	}
	var body struct {
		Name      string `json:"name"`
		GameID    string `json:"gameid"`
		WorldName string `json:"worldname"`
		Port      int    `json:"port"`
		AdminName string `json:"admin_name"`
		MgName    string `json:"mg_name"`
		Seed      string `json:"seed"`
	}
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.GameID = strings.TrimSpace(body.GameID)
	body.WorldName = strings.TrimSpace(body.WorldName)
	body.AdminName = strings.TrimSpace(body.AdminName)
	body.Seed = strings.TrimSpace(body.Seed)
	if body.Name == "" || len(body.Name) > 60 {
		writeErr(w, 400, "实例名称不能为空（60 字符内）")
		return
	}
	if !luanti.IsValidPlayerName(body.AdminName) {
		writeErr(w, 400, "管理员玩家名不合法（2-20 位字母数字_-）")
		return
	}
	if !validDirName(body.WorldName) {
		writeErr(w, 400, "世界名称只能包含字母、数字、_ 和 -")
		return
	}
	if body.GameID == "" {
		writeErr(w, 400, "必须选择游戏（Luanti 5.8+ 不再自带默认游戏）")
		return
	}
	if body.Port == 0 {
		body.Port = 30000
	}
	if body.Port < 1 || body.Port > 65535 {
		writeErr(w, 400, "端口必须在 1-65535")
		return
	}
	if used, _ := s.st.PortInUseByOtherInstance(body.Port, ""); used {
		writeErr(w, 400, fmt.Sprintf("端口 %d 已被其他实例占用", body.Port))
		return
	}
	if !luanti.UdpPortFree(body.Port) {
		writeErr(w, 400, fmt.Sprintf("UDP 端口 %d 已被系统占用", body.Port))
		return
	}
	if body.Seed != "" && (len(body.Seed) > 20 || !isDigits(body.Seed)) {
		writeErr(w, 400, "地图种子应为不超过 20 位的数字")
		return
	}

	id := store.RandomToken(4)
	d := luanti.Dirs{Root: filepath.Join(s.dataDir, "instances", id)}
	if err := d.EnsureLayout(body.WorldName); err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	// 游戏就位：实例 games/<gameid> 缺失时从全局游戏库复制
	if err := s.ensureGame(id, body.GameID); err != nil {
		writeErr(w, 400, err.Error())
		return
	}

	// 面板桥接地址：本机回环 + 面板端口
	panelPort := s.st.GetSetting("panel_port", "8080")
	bridgeURL := "http://127.0.0.1:" + panelPort

	bridgeToken := store.RandomToken(24)
	conf := luanti.BaseConf(body.Port, body.AdminName, bridgeURL, bridgeToken, body.MgName, body.Seed)
	if err := os.WriteFile(d.Config(), []byte(conf), 0o644); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	wmtPath := filepath.Join(d.World(body.WorldName), "world.mt")
	wmt, _ := luanti.LoadWorldMT(wmtPath)
	wmt.Set("world_name", body.WorldName)
	wmt.Set("gameid", body.GameID)
	wmt.Set("backend", "sqlite3")
	wmt.Set("player_backend", "sqlite3")
	wmt.Set("auth_backend", "sqlite3")
	if err := wmt.Save(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := s.RT.EnsureBridgeMod(d.World(body.WorldName)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}

	rec := &store.Instance{
		ID: id, Name: body.Name, Port: body.Port,
		GameID: body.GameID, WorldName: body.WorldName, AdminName: body.AdminName,
		AutoRestart: true, BackupKeepCount: 10, BridgeToken: bridgeToken,
	}
	if err := s.st.CreateInstance(rec); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.create", id, "创建实例 "+body.Name+" (端口 "+strconv.Itoa(body.Port)+")")
	writeJSON(w, 200, map[string]any{"id": id, "hint": "请在防火墙放行 UDP 端口 " + strconv.Itoa(body.Port)})
}

func validDirName(s string) bool {
	if s == "" || len(s) > 60 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ensureGame 保证实例拥有指定游戏；全局库有则复制，否则报错引导安装。
func (s *Server) ensureGame(instanceID, gameID string) error {
	d := s.RT.Dirs(instanceID)
	dst := filepath.Join(d.Games(), gameID)
	if _, err := os.Stat(filepath.Join(dst, "game.conf")); err == nil {
		return nil
	}
	lib := filepath.Join(s.dataDir, "games", gameID)
	if _, err := os.Stat(filepath.Join(lib, "game.conf")); err != nil {
		return fmt.Errorf("游戏 %s 未安装：请先在「Mod 管理 → 游戏库」安装，或从 ContentDB 安装后重试", gameID)
	}
	if err := copyDir(lib, dst); err != nil {
		return fmt.Errorf("复制游戏失败: %w", err)
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

// ---- 更新 ----

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Name                *string `json:"name"`
		AutoRestart         *bool   `json:"auto_restart"`
		BackupIntervalHours *int    `json:"backup_interval_hours"`
		BackupKeepCount     *int    `json:"backup_keep_count"`
	}
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.BackupIntervalHours != nil && (*body.BackupIntervalHours < 0 || *body.BackupIntervalHours > 720) {
		writeErr(w, 400, "备份间隔需在 0-720 小时")
		return
	}
	if body.BackupKeepCount != nil && (*body.BackupKeepCount < 1 || *body.BackupKeepCount > 100) {
		writeErr(w, 400, "备份保留份数需在 1-100")
		return
	}
	if err := s.st.UpdateInstance(i.ID, &store.InstanceUpdate{
		Name: body.Name, AutoRestart: body.AutoRestart,
		BackupIntervalHours: body.BackupIntervalHours, BackupKeepCount: body.BackupKeepCount,
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if body.AutoRestart != nil {
		if p, err := s.RT.Proc(i.ID); err == nil {
			p.SetAutoRestart(*body.AutoRestart)
		}
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.update", i.ID, "更新实例设置")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	if status := s.RT.StatusOf(i.ID); status == "running" || status == "starting" {
		if err := s.RT.Stop(i.ID); err != nil {
			writeErr(w, 500, "停止实例失败: "+err.Error())
			return
		}
	}
	dir := s.RT.Dirs(i.ID).Root
	if err := os.RemoveAll(dir); err != nil {
		writeErr(w, 500, "删除目录失败: "+err.Error())
		return
	}
	if err := s.st.DeleteInstance(i.ID); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.delete", i.ID, "删除实例 "+i.Name)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- 进程控制 ----

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	if err := s.RT.Start(i.ID); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.start", i.ID, "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	if err := s.RT.Stop(i.ID); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.stop", i.ID, "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	if err := s.RT.Restart(i.ID); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.restart", i.ID, "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- 克隆 ----

func (s *Server) handleClone(w http.ResponseWriter, r *http.Request) {
	src := inst(r)
	var body struct {
		Name      string `json:"name"`
		Port      int    `json:"port"`
		CopyWorld bool   `json:"copy_world"`
	}
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = src.Name + " 副本"
	}
	port := body.Port
	if port == 0 {
		port = s.nextFreePort()
	}
	if used, _ := s.st.PortInUseByOtherInstance(port, ""); used || !luanti.UdpPortFree(port) {
		writeErr(w, 400, fmt.Sprintf("端口 %d 不可用", port))
		return
	}

	id := store.RandomToken(4)
	sd := s.RT.Dirs(src.ID)
	dd := luanti.Dirs{Root: filepath.Join(s.dataDir, "instances", id)}
	world := src.WorldName
	if !body.CopyWorld {
		world = src.WorldName + "_clone"
	}
	if !validDirName(world) {
		writeErr(w, 400, "世界名不合法")
		return
	}
	if err := dd.EnsureLayout(world); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	for _, pair := range [][2]string{
		{sd.Mods(), dd.Mods()}, {sd.Games(), dd.Games()}, {sd.ConfigDir(), dd.ConfigDir()},
	} {
		if _, err := os.Stat(pair[0]); err == nil {
			if err := copyDir(pair[0], pair[1]); err != nil {
				writeErr(w, 500, "复制失败: "+err.Error())
				return
			}
		}
	}
	if body.CopyWorld {
		if err := copyDir(sd.World(src.WorldName), dd.World(world)); err != nil {
			writeErr(w, 500, "复制世界失败: "+err.Error())
			return
		}
		// 克隆的世界改个名字避免重名
		wmt, _ := luanti.LoadWorldMT(filepath.Join(dd.World(world), "world.mt"))
		wmt.Set("world_name", world)
		_ = wmt.Save()
	} else {
		wmt, _ := luanti.LoadWorldMT(filepath.Join(dd.World(world), "world.mt"))
		wmt.Set("world_name", world)
		wmt.Set("gameid", src.GameID)
		wmt.Set("backend", "sqlite3")
		wmt.Set("player_backend", "sqlite3")
		wmt.Set("auth_backend", "sqlite3")
		_ = wmt.Save()
		s.RT.EnsureBridgeMod(dd.World(world))
	}

	bridgeToken := store.RandomToken(24)
	rec := &store.Instance{
		ID: id, Name: name, Port: port, GameID: src.GameID, WorldName: world,
		AdminName: src.AdminName, AutoRestart: src.AutoRestart,
		BackupIntervalHours: src.BackupIntervalHours, BackupKeepCount: src.BackupKeepCount,
		BridgeToken: bridgeToken,
	}
	if err := s.st.CreateInstance(rec); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 克隆实例的 conf 需要独立 token 与端口
	if content, err := luanti.ReadConfFile(dd.Config()); err == nil {
		panelPort := s.st.GetSetting("panel_port", "8080")
		merged, _ := luanti.MergeValues(content, map[string]string{
			"port":                 strconv.Itoa(port),
			"panel_bridge.url":     "http://127.0.0.1:" + panelPort,
			"panel_bridge.token":   bridgeToken,
		})
		_ = luanti.WriteConfFile(dd.Config(), merged)
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "instance.clone", src.ID, "克隆为 "+id+"（"+name+"）")
	writeJSON(w, 200, map[string]string{"id": id})
}

func (s *Server) nextFreePort() int {
	insts, _ := s.st.ListInstances()
	used := map[int]bool{}
	for _, i := range insts {
		used[i.Port] = true
	}
	for p := 30000; p < 40000; p++ {
		if !used[p] && luanti.UdpPortFree(p) {
			return p
		}
	}
	return 0
}
