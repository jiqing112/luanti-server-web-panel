package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"luantipanel/internal/auth"
	"luantipanel/internal/filesys"
)

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	root := s.RT.Dirs(i.ID).Root
	rel := r.URL.Query().Get("path")
	entries, err := filesys.ListDir(root, rel)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": rel, "entries": entries})
}

func (s *Server) handleFileRead(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	rel := r.URL.Query().Get("path")
	content, err := filesys.ReadText(s.RT.Dirs(i.ID).Root, rel)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"path": rel, "content": content})
}

func (s *Server) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := readBody(r, &body); err != nil || body.Path == "" {
		writeErr(w, 400, "缺少 path")
		return
	}
	if err := filesys.WriteText(s.RT.Dirs(i.ID).Root, body.Path, body.Content); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "file.write", i.ID, body.Path)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, 400, "上传解析失败")
		return
	}
	rel := r.FormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "缺少文件字段 file")
		return
	}
	defer file.Close()
	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." || strings.Contains(name, "/") {
		writeErr(w, 400, "非法文件名")
		return
	}
	target := filepath.ToSlash(filepath.Join(rel, name))
	if err := filesys.Upload(s.RT.Dirs(i.ID).Root, target, file, filesys.MaxUploadSize); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "file.upload", i.ID, target)
	writeJSON(w, 200, map[string]any{"ok": true, "path": target})
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	var body struct {
		Path string `json:"path"`
	}
	if err := readBody(r, &body); err != nil || body.Path == "" {
		writeErr(w, 400, "缺少 path")
		return
	}
	if err := filesys.MkdirAll(s.RT.Dirs(i.ID).Root, body.Path); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	rel := r.URL.Query().Get("path")
	if rel == "" || rel == "." || rel == "/" {
		writeErr(w, 400, "不能删除根目录")
		return
	}
	if err := filesys.Delete(s.RT.Dirs(i.ID).Root, rel); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "file.delete", i.ID, rel)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	i := inst(r)
	rel := r.URL.Query().Get("path")
	f, fi, err := filesys.Open(s.RT.Dirs(i.ID).Root, rel)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(rel)))
	http.ServeContent(w, r, filepath.Base(rel), fi.ModTime(), f)
}
