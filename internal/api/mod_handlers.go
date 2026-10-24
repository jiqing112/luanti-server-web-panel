package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"luantipanel/internal/auth"
	"luantipanel/internal/contentdb"
	"luantipanel/internal/luanti"
)

func jsonDecode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// ---- 已安装 Mod / 游戏 ----

type ModInfo struct {
	Name    string `json:"name"`
	Enabled string `json:"enabled"` // world.mt 中的原始值（true / mods/x / false / 未设置）
	Loaded  bool   `json:"loaded"`
	FromGame bool  `json:"from_game"`
	Title   string `json:"title"`
}

func (s *Server) handleListMods(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)

	wmt, err := luanti.LoadWorldMT(filepath.Join(d.World(i.WorldName), "world.mt"))
	if err != nil {
		writeErr(w, 500, "读取 world.mt 失败: "+err.Error())
		return
	}
	loads := wmt.LoadMods()

	out := []*ModInfo{}
	// 实例 mods/ 目录
	entries, _ := os.ReadDir(d.Mods())
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mi := &ModInfo{Name: e.Name()}
		if v, ok := loads[e.Name()]; ok {
			mi.Enabled = v
			mi.Loaded = v != "false"
		} else {
			mi.Enabled = "未配置"
			mi.Loaded = false
		}
		if kv, err := luanti.ParseKVFile(filepath.Join(d.Mods(), e.Name(), "mod.conf")); err == nil {
			mi.Title = kv["title"]
			if mi.Title == "" {
				mi.Title = kv["name"]
			}
		}
		out = append(out, mi)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })

	// 游戏自带 mod（只读展示）
	gameMods := []*ModInfo{}
	if g := findGameDir(d.Games(), i.GameID); g != "" {
		gm := filepath.Join(g, "mods")
		gEntries, _ := os.ReadDir(gm)
		for _, e := range gEntries {
			if !e.IsDir() {
				continue
			}
			mi := &ModInfo{Name: e.Name(), FromGame: true, Loaded: true, Enabled: "game"}
			if kv, err := luanti.ParseKVFile(filepath.Join(gm, e.Name(), "mod.conf")); err == nil && kv["title"] != "" {
				mi.Title = kv["title"]
			}
			gameMods = append(gameMods, mi)
		}
		sort.Slice(gameMods, func(a, b int) bool { return gameMods[a].Name < gameMods[b].Name })
	}

	writeJSON(w, 200, map[string]any{"mods": out, "game_mods": gameMods})
}

func findGameDir(gamesDir, gameID string) string {
	p := filepath.Join(gamesDir, gameID)
	if _, err := os.Stat(filepath.Join(p, "game.conf")); err == nil {
		return p
	}
	return ""
}

// toggleMod 更新 world.mt 中的 load_mod 条目。
func (s *Server) toggleMod(instanceID string, modName string, enabled bool) error {
	d := s.RT.Dirs(instanceID)
	world, err := s.worldOf(instanceID)
	if err != nil {
		return err
	}
	wmtPath := filepath.Join(d.World(world), "world.mt")
	wmt, err := luanti.LoadWorldMT(wmtPath)
	if err != nil {
		return err
	}
	wmt.SetLoadMod(modName, enabled)
	return wmt.Save()
}

// worldOf 取实例的世界名（DB 行）。
func (s *Server) worldOf(instanceID string) (string, error) {
	i, err := s.st.GetInstance(instanceID)
	if err != nil {
		return "", err
	}
	return i.WorldName, nil
}

func (s *Server) handleModToggle(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	name := pathParam(r, "name")
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readBody(r, &body); err != nil || body.Enabled == nil {
		writeErr(w, 400, "缺少 enabled")
		return
	}
	if !validDirName(name) {
		writeErr(w, 400, "mod 名不合法")
		return
	}
	if err := s.toggleMod(i.ID, name, *body.Enabled); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "mod.toggle", i.ID, name+" → "+fmt.Sprint(*body.Enabled))
	writeJSON(w, 200, map[string]any{"ok": true, "hint": "重启实例后生效"})
}

// ---- ContentDB ----

// handleContentDBSearch ContentDB 搜索代理。
func (s *Server) handleContentDBSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	typ := r.URL.Query().Get("type")
	limit := queryInt(r, "limit", 20)
	pkgs, err := s.RT.CDB.Search(q, typ, limit)
	if err != nil {
		writeErr(w, 502, "ContentDB 请求失败: "+err.Error())
		return
	}
	writeJSON(w, 200, pkgs)
}

// handleContentDBPackage 透传包详情与截图列表（ContentDB 模态框用）。
func (s *Server) handleContentDBPackage(w http.ResponseWriter, r *http.Request) {
	author := r.URL.Query().Get("author")
	name := r.URL.Query().Get("name")
	if author == "" || name == "" ||
		strings.ContainsAny(author+name, "/\\") || strings.Contains(author+name, "..") {
		writeErr(w, 400, "参数不合法")
		return
	}
	base := "/api/packages/" + url.PathEscape(author) + "/" + url.PathEscape(name)
	detail, err := s.RT.CDB.GetRaw(base + "/")
	if err != nil {
		writeErr(w, 502, "获取包详情失败: "+err.Error())
		return
	}
	shots, err := s.RT.CDB.GetRaw(base + "/screenshots/")
	if err != nil {
		shots = json.RawMessage("[]")
	}
	writeJSON(w, 200, map[string]json.RawMessage{"detail": detail, "screenshots": shots})
}

// handleModInstall 从 ContentDB 安装 mod（含依赖解析）到实例 mods/。
func (s *Server) handleModInstall(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Author      string `json:"author"`
		Name        string `json:"name"`
		ResolveDeps *bool  `json:"resolve_deps"`
	}
	if err := readBody(r, &body); err != nil || body.Author == "" || body.Name == "" {
		writeErr(w, 400, "缺少 author/name")
		return
	}
	d := s.RT.Dirs(i.ID)

	have := func(modname string) bool {
		// 1. 游戏自带
		if g := findGameDir(d.Games(), i.GameID); g != "" {
			if _, err := os.Stat(filepath.Join(g, "mods", modname)); err == nil {
				return true
			}
		}
		// 2. 已安装
		if _, err := os.Stat(filepath.Join(d.Mods(), modname)); err == nil {
			return true
		}
		return false
	}

	// 解析安装清单（根包 + 依赖）
	root := contentdb.Package{Author: body.Author, Name: body.Name}
	pkgs := []contentdb.Package{root}
	if body.ResolveDeps == nil || *body.ResolveDeps {
		list, err := s.RT.CDB.ResolveDeps(body.Author, body.Name, have)
		if err != nil {
			writeErr(w, 502, "依赖解析失败: "+err.Error())
			return
		}
		if len(list) > 0 {
			pkgs = list
		}
	}

	installed, skipped := []string{}, []string{}
	for _, pkg := range pkgs {
		if have(pkg.Name) && !(pkg.Author == body.Author && pkg.Name == body.Name) {
			skipped = append(skipped, pkg.Name)
			continue
		}
		name, err := s.RT.CDB.InstallZip(pkg, d.Mods())
		if err != nil {
			writeErr(w, 502, fmt.Sprintf("安装 %s 失败: %v", pkg.FullName(), err))
			return
		}
		if name == "" {
			name = pkg.Name
		}
		installed = append(installed, name)
		// 写入 world.mt
		if err := s.toggleMod(i.ID, name, true); err != nil {
			writeErr(w, 500, "写入 world.mt 失败: "+err.Error())
			return
		}
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "mod.install", i.ID, strings.Join(installed, ", "))
	writeJSON(w, 200, map[string]any{
		"installed": installed, "skipped": skipped,
		"hint": "安装完成并已写入 world.mt，重启实例后生效",
	})
}

// ---- 游戏库（全局） ----

// cdbMeta 游戏库条目的 ContentDB 元数据（记录来源与 release，用于更新检查）。
type cdbMeta struct {
	Author      string `json:"author"`
	Name        string `json:"name"`
	Release     int64  `json:"release"`
	InstalledAt string `json:"installed_at"`
}

const cdbMetaFile = ".cdb-meta.json"

func readCDBMeta(libDir, id string) *cdbMeta {
	b, err := os.ReadFile(filepath.Join(libDir, id, cdbMetaFile))
	if err != nil {
		return nil
	}
	var m cdbMeta
	if json.Unmarshal(b, &m) != nil || m.Author == "" {
		return nil
	}
	return &m
}

// handleGamesLibrary 列出全局库与各实例游戏，用于创建实例时的选择。
func (s *Server) handleGamesLibrary(w http.ResponseWriter, r *http.Request) {
	libDir := filepath.Join(s.dataDir, "games")
	lib, err := luanti.ListGames(libDir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type libGame struct {
		ID    string   `json:"id"`
		Title string   `json:"title"`
		Meta  *cdbMeta `json:"meta"`
	}
	out := make([]libGame, 0, len(lib))
	for _, g := range lib {
		out = append(out, libGame{ID: g.ID, Title: g.Title, Meta: readCDBMeta(libDir, g.ID)})
	}
	insts, _ := s.st.ListInstances()
	perInstance := map[string][]luanti.Game{}
	for _, i := range insts {
		gs, _ := luanti.ListGames(s.RT.Dirs(i.ID).Games())
		perInstance[i.ID] = gs
	}
	writeJSON(w, 200, map[string]any{"library": out, "instances": perInstance})
}

// handleGamesLibraryInstall 安装（或 force 更新）游戏到全局库。
func (s *Server) handleGamesLibraryInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Author string `json:"author"`
		Name   string `json:"name"`
		Force  bool   `json:"force"`
	}
	if err := readBody(r, &body); err != nil || body.Author == "" || body.Name == "" {
		writeErr(w, 400, "缺少 author/name")
		return
	}
	if !validDirName(body.Name) {
		writeErr(w, 400, "包名不合法")
		return
	}
	libDir := filepath.Join(s.dataDir, "games")
	dst := filepath.Join(libDir, body.Name)
	if _, err := os.Stat(filepath.Join(dst, "game.conf")); err == nil && !body.Force {
		writeErr(w, 400, "游戏库中已存在该游戏（如需更新请使用更新操作）")
		return
	}
	rel, err := s.latestReleaseID(body.Author, body.Name)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	pkg := contentdb.Package{Author: body.Author, Name: body.Name, Release: rel}

	// 先下载解压到暂存目录，成功后再替换旧版本：
	// 旧实现先 RemoveAll 再下载，下载失败会把游戏库里的旧版本一并丢掉
	stageDir, err := os.MkdirTemp(libDir, ".stage-*")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer os.RemoveAll(stageDir)
	name, err := s.RT.CDB.InstallZip(pkg, stageDir)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	src := filepath.Join(stageDir, name)
	if name == "" {
		src = stageDir
	}

	// 换入新版本（旧版本先改名，失败可回滚）
	old := ""
	if _, err := os.Stat(dst); err == nil {
		old = dst + ".old-" + time.Now().Format("20060102150405")
		if err := os.Rename(dst, old); err != nil {
			writeErr(w, 500, "备份旧版本失败: "+err.Error())
			return
		}
	}
	if err := os.Rename(src, dst); err != nil {
		if err := copyDir(src, dst); err != nil {
			if old != "" {
				_ = os.Rename(old, dst) // 回滚
			}
			writeErr(w, 500, "安装失败: "+err.Error())
			return
		}
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}

	// 写入元数据（供更新检查）
	meta, _ := json.Marshal(cdbMeta{
		Author: body.Author, Name: body.Name, Release: rel,
		InstalledAt: time.Now().Format(time.RFC3339),
	})
	_ = os.WriteFile(filepath.Join(dst, cdbMetaFile), meta, 0o644)
	u := auth.CurrentUser(r)
	action := "game.install"
	if body.Force {
		action = "game.update"
	}
	s.st.Audit(u.Username, action, "", body.Author+"/"+body.Name+" (release "+strconv.FormatInt(rel, 10)+")")
	writeJSON(w, 200, map[string]any{"ok": true, "gameid": body.Name, "release": rel})
}

// handleGamesLibraryCheckUpdates 检查游戏库中已装游戏的最新 release。
func (s *Server) handleGamesLibraryCheckUpdates(w http.ResponseWriter, r *http.Request) {
	libDir := filepath.Join(s.dataDir, "games")
	games, err := luanti.ListGames(libDir)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type item struct {
		GameID        string   `json:"gameid"`
		Title         string   `json:"title"`
		Meta          *cdbMeta `json:"meta"`
		HasUpdate     bool     `json:"has_update"`
		LatestRelease int64    `json:"latest_release"`
		Error         string   `json:"error,omitempty"`
	}
	items := make([]item, 0, len(games))
	for _, g := range games {
		m := readCDBMeta(libDir, g.ID)
		if m == nil {
			// 手动放入或旧版安装，无元数据
			items = append(items, item{GameID: g.ID, Title: g.Title})
			continue
		}
		pkg, err := s.RT.CDB.GetPackage(m.Author, m.Name)
		if err != nil {
			items = append(items, item{GameID: g.ID, Title: g.Title, Meta: m, Error: err.Error()})
			continue
		}
		items = append(items, item{
			GameID: g.ID, Title: g.Title, Meta: m,
			HasUpdate:     pkg.Release > m.Release,
			LatestRelease: pkg.Release,
		})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

// handleGamesLibraryDelete 从全局库删除游戏。
func (s *Server) handleGamesLibraryDelete(w http.ResponseWriter, r *http.Request) {
	id := pathParam(r, "gameid")
	if !validDirName(id) {
		writeErr(w, 400, "非法 gameid")
		return
	}
	dir := filepath.Join(s.dataDir, "games", id)
	if _, err := os.Stat(filepath.Join(dir, "game.conf")); err != nil {
		writeErr(w, 404, "游戏库中不存在该游戏")
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "game.delete", "", id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// latestReleaseID 查询包的最新 release id。
func (s *Server) latestReleaseID(author, name string) (int64, error) {
	pkg, err := s.RT.CDB.GetPackage(author, name)
	if err != nil {
		return 0, err
	}
	if pkg.Release <= 0 {
		return 0, fmt.Errorf("无法获取 %s/%s 的 release", author, name)
	}
	return pkg.Release, nil
}

// handleInstanceGames 列出实例已安装的游戏。
func (s *Server) handleInstanceGames(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	gs, err := luanti.ListGames(s.RT.Dirs(i.ID).Games())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, gs)
}
