package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"luantipanel/internal/auth"
	"luantipanel/internal/luanti"
	"luantipanel/internal/store"
)

// allowedSettings 仅暴露确实生效的设置项。
// panel_port 由 -addr 启动参数自动写入；崩溃阈值/冷却为内置常量，
// 均不接受 API 修改（旧列表中的 alert_crash_threshold 从未生效）。
var allowedSettings = map[string]bool{
	"webhook_url":        true,
	"webhook_type":       true,
	"binary_repo":        true,
	"luanti_binary_path": true,
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := s.st.AllSettings()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	// 确保关键键有默认值展示
	if _, ok := all["binary_repo"]; !ok {
		all["binary_repo"] = "ROllerozxa/luantiserver"
	}
	writeJSON(w, 200, all)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	for k, v := range body {
		if !allowedSettings[k] {
			writeErr(w, 400, "不支持的设置项: "+k)
			return
		}
		if err := s.st.SetSetting(k, strings.TrimSpace(v)); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	if v, ok := body["webhook_url"]; ok {
		typ := s.st.GetSetting("webhook_type", "discord")
		if t, ok2 := body["webhook_type"]; ok2 && t != "" {
			typ = t
		}
		s.Notify.Configure(v, typ)
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "settings.update", "", "")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---- 二进制管理 ----

// manualBinaryPath 返回管理员指定的二进制路径（设置项），可能为空。
func (s *Server) manualBinaryPath() string {
	p := strings.TrimSpace(s.st.GetSetting("luanti_binary_path", ""))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err == nil {
			return abs
		}
	}
	return p
}

func (s *Server) handleBinaryInfo(w http.ResponseWriter, r *http.Request) {
	manual := s.manualBinaryPath()
	master := s.Bin.BinaryPath()
	info := map[string]any{
		"master_path":      master,
		"manual_path":      manual,
		"latest_version":   s.st.GetSetting("binary_latest", ""),
		"binary_repo":      s.st.GetSetting("binary_repo", "ROllerozxa/luantiserver"),
	}

	check := func(path string) {
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			info["exists"] = true
			info["size_mb"] = float64(fi.Size()) / 1024 / 1024
			if v, err := s.Bin.ProbeVersion(path); err == nil {
				info["version"] = v
			} else {
				info["version_error"] = err.Error()
			}
		} else {
			info["exists"] = false
		}
	}
	if manual != "" {
		check(manual)
		info["active_path"] = manual
	} else {
		check(master)
		info["active_path"] = master
	}
	writeJSON(w, 200, info)
}

func (s *Server) handleBinaryCheck(w http.ResponseWriter, r *http.Request) {
	repo := s.st.GetSetting("binary_repo", "ROllerozxa/luantiserver")
	rel, err := s.Bin.LatestRelease(repo)
	if err != nil {
		writeErr(w, 502, "查询失败: "+err.Error())
		return
	}
	_ = s.st.SetSetting("binary_latest", strings.TrimPrefix(rel.Tag, "v"))
	_ = s.st.SetSetting("binary_latest_url", rel.HTMLURL)
	writeJSON(w, 200, map[string]any{
		"latest_version": strings.TrimPrefix(rel.Tag, "v"),
		"url":            rel.HTMLURL,
	})
}

func (s *Server) handleBinaryDownload(w http.ResponseWriter, r *http.Request) {
	repo := s.st.GetSetting("binary_repo", "ROllerozxa/luantiserver")
	ver, err := s.Bin.Download(repo)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	_ = s.st.SetSetting("binary_version", ver)
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "binary.download", "", ver)
	writeJSON(w, 200, map[string]any{"ok": true, "version": ver})
}

// ---- schema 刷新 ----

func (s *Server) handleSchemaRefresh(w http.ResponseWriter, r *http.Request) {
	example := s.Bin.ExampleConfPath()
	if _, err := os.Stat(example); err != nil {
		// 尝试从 GitHub 拉取
		if err := s.Bin.FetchExampleConf(); err != nil {
			writeErr(w, 502, "minetest.conf.example 不可用（可先下载二进制）: "+err.Error())
			return
		}
	}
	b, err := os.ReadFile(example)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rows := luanti.ParseExampleConf(string(b))
	if len(rows) == 0 {
		writeErr(w, 500, "解析结果为空，example 文件可能损坏")
		return
	}
	if err := s.st.ReplaceSchema(rows); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	u := auth.CurrentUser(r)
	s.st.Audit(u.Username, "schema.refresh", "", "")
	writeJSON(w, 200, map[string]any{"ok": true, "count": len(rows)})
}

// ---- 审计 ----

func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	page := queryInt(r, "page", 1)
	size := queryInt(r, "page_size", 50)
	if size > 200 {
		size = 200
	}
	if page < 1 {
		page = 1
	}
	rows, total, err := s.st.ListAudit(r.URL.Query().Get("instance_id"), size, (page-1)*size)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": rows, "total": total, "page": page, "page_size": size})
}

var _ = store.ErrNotFound
