package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"luantipanel/internal/auth"
	"luantipanel/internal/backup"
)

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	// 运行中先经 bridge 发 /save，降低热备份拷到撕裂 SQLite 状态的概率
	if status := s.RT.StatusOf(i.ID); status == "running" {
		_, _ = s.RT.Command(i.ID, "/save")
	}
	m, err := backup.CreateWorldBackup(d, i.WorldName)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "backup.create", i.ID, m.Name)
	writeJSON(w, 200, m)
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	writeJSON(w, 200, backup.ListWorldBackups(d))
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	name := pathParam(r, "name")
	if !strings.HasSuffix(name, ".zip") || strings.ContainsAny(name, "/\\") {
		writeErr(w, 400, "非法备份名")
		return
	}
	if status := s.RT.StatusOf(i.ID); status == "running" || status == "starting" || status == "restarting" {
		if err := s.RT.Stop(i.ID); err != nil {
			writeErr(w, 500, "停止实例失败: "+err.Error())
			return
		}
	}
	if err := backup.RestoreWorldBackup(d, i.WorldName, name); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "backup.restore", i.ID, name)
	writeJSON(w, 200, map[string]any{"ok": true, "hint": "恢复完成，实例已停止，可手动启动"})
}

func (s *Server) handleLockBackup(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	var body struct {
		Locked *bool `json:"locked"`
	}
	if err := readBody(r, &body); err != nil || body.Locked == nil {
		writeErr(w, 400, "缺少 locked")
		return
	}
	name := pathParam(r, "name")
	for _, m := range backup.ListWorldBackups(d) {
		if m.Name == name {
			if err := m.SetLock(d, *body.Locked); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	writeErr(w, 404, "备份不存在")
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	name := pathParam(r, "name")
	for _, m := range backup.ListWorldBackups(d) {
		if m.Name == name {
			if m.Locked {
				writeErr(w, 400, "该备份已锁定，请先解除锁定")
				return
			}
			if err := backup.DeleteWorldBackup(d, name); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			u := auth.CurrentUser(r)
			s.st.Audit(u.Username, "backup.delete", i.ID, name)
			writeJSON(w, 200, map[string]bool{"ok": true})
			return
		}
	}
	writeErr(w, 404, "备份不存在")
}

func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	d := s.RT.Dirs(i.ID)
	name := pathParam(r, "name")
	if !strings.HasSuffix(name, ".zip") || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		writeErr(w, 400, "非法备份名")
		return
	}
	p := filepath.Join(d.BackupsWorld(), name)
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, 404, "备份不存在")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s-%s\"", i.Name, name))
	http.ServeContent(w, r, name, fi.ModTime(), f)
}
