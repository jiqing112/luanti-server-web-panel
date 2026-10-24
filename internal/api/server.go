// Package api: HTTP 路由与处理器。
package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"luantipanel/internal/api/spa"
	"luantipanel/internal/auth"
	"luantipanel/internal/luantibin"
	"luantipanel/internal/notify"
	"luantipanel/internal/runtime"
	"luantipanel/internal/store"
	"luantipanel/web"
)

type Options struct {
	Store   *store.Store
	DataDir string
	Version string
	Addr    string
}

type Server struct {
	st      *store.Store
	dataDir string
	Version string

	Auth *auth.Manager
	RT   *runtime.Runtime
	Notify *notify.Notifier
	Bin  *luantibin.Manager
}

func New(o Options) *Server {
	n := notify.New()
	s := &Server{
		st:      o.Store,
		dataDir: o.DataDir,
		Version: o.Version,
		Auth:    auth.New(o.Store),
		Notify:  n,
		Bin:     luantibin.New(o.DataDir),
	}
	s.RT = runtime.New(o.Store, o.DataDir, n)

	// 面板端口（bridge 回调地址用），随 -addr 同步
	if _, port, err := net.SplitHostPort(o.Addr); err == nil && port != "" {
		_ = s.st.SetSetting("panel_port", port)
	}

	// 恢复面板启动前的状态标记：所有实例标记为 stopped
	_ = s.applyStartupStatus()

	// 从设置恢复 webhook 配置
	if url := s.st.GetSetting("webhook_url", ""); url != "" {
		n.Configure(url, s.st.GetSetting("webhook_type", "discord"))
	}
	return s
}

func (s *Server) applyStartupStatus() error {
	insts, err := s.st.ListInstances()
	if err != nil {
		return err
	}
	for _, i := range insts {
		if i.Status != "stopped" {
			st := "stopped"
			if err := s.st.UpdateInstance(i.ID, &store.InstanceUpdate{Status: &st}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) Shutdown() { s.RT.Shutdown() }

// Handler 构建完整 HTTP 路由。
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(realIP)
	r.Use(middleware.Compress(5))
	r.Use(recoverer)

	// 嵌入的 SPA（放在 /api 之前，静态资源直接命中）
	staticFS := spa.Handler(web.Dist(), "index.html")
	r.Handle("/*", staticFS)
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/instances", http.StatusTemporaryRedirect)
	})

	r.Route("/api", func(r chi.Router) {
		// 公开
		r.Post("/auth/login", s.handleLogin)

		// bridge（服务端 mod 调用，token 认证）
		r.Post("/bridge/poll", s.handleBridgePoll)
		r.Post("/bridge/result", s.handleBridgeResult)

		// 以下全部需要登录
		r.Group(func(r chi.Router) {
			r.Use(s.Auth.LoadSession)
			r.Use(auth.RequireAuth)

			r.Get("/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)

			r.Get("/instances", s.handleListInstances)
			r.Get("/instances/next-port", s.handleNextPort)
			r.Post("/instances", s.handleCreateInstance) // admin 在内部校验
			r.Route("/instances/{id}", func(r chi.Router) {
				r.Use(s.instanceCtx)

				r.Get("/", s.handleGetInstance)
				r.Put("/", s.handleUpdateInstance)
				r.Delete("/", s.requireAdmin(s.handleDeleteInstance))
				r.Post("/start", s.handleStart)
				r.Post("/stop", s.handleStop)
				r.Post("/restart", s.handleRestart)
				r.Post("/clone", s.requireAdmin(s.handleClone))

				// 配置
				r.Get("/config", s.handleGetConfig)
				r.Put("/config", s.requireAdmin(s.handlePutConfig))
				r.Get("/config/schema", s.handleConfigSchema)
				r.Get("/config/history", s.handleConfigHistory)
				r.Get("/config/diff", s.handleConfigDiff)
				r.Post("/config/restore", s.requireAdmin(s.handleConfigRestore))

				// 游戏 / Mod
				r.Get("/games", s.handleInstanceGames)
				r.Get("/mods", s.handleListMods)
				r.Post("/mods/install", s.requireAdmin(s.handleModInstall))
				r.Put("/mods/{name}/toggle", s.requireAdmin(s.handleModToggle))

				// 玩家与审计
				r.Get("/players", s.handlePlayers)
				r.Get("/admin", s.requireAdmin(s.handleAdminGet))
				r.Post("/admin/password", s.requireAdmin(s.handleAdminPassword))
				r.Get("/activity", s.handleActivity)
				r.Get("/blocklog", s.handleBlockLog)
				r.Post("/command", s.handleCommand)

				// 备份
				r.Post("/backup", s.handleCreateBackup)
				r.Get("/backups", s.handleListBackups)
				r.Post("/backups/{name}/restore", s.requireAdmin(s.handleRestoreBackup))
				r.Post("/backups/{name}/lock", s.handleLockBackup)
				r.Delete("/backups/{name}", s.requireAdmin(s.handleDeleteBackup))
				r.Get("/backups/{name}/download", s.handleDownloadBackup)

				// 文件
				r.Get("/files", s.handleFileList)
				r.Get("/files/content", s.handleFileRead)
				r.Put("/files/content", s.requireAdmin(s.handleFileWrite))
				r.Post("/files/upload", s.requireAdmin(s.handleFileUpload))
				r.Post("/files/mkdir", s.requireAdmin(s.handleFileMkdir))
				r.Delete("/files", s.requireAdmin(s.handleFileDelete))
				r.Get("/files/download", s.handleFileDownload)

				// WebSocket 日志流
				r.Get("/logs", s.handleInstanceWS)
			})

			// 全局游戏库 / ContentDB
			r.Get("/games/library", s.handleGamesLibrary)
			r.Post("/games/library/install", s.requireAdmin(s.handleGamesLibraryInstall))
			r.Post("/games/library/check-updates", s.handleGamesLibraryCheckUpdates)
			r.Delete("/games/library/{gameid}", s.requireAdmin(s.handleGamesLibraryDelete))
			r.Get("/contentdb/search", s.handleContentDBSearch)
			r.Get("/contentdb/package", s.handleContentDBPackage)

			// 用户管理（admin）
			r.Get("/users", s.requireAdmin(s.handleListUsers))
			r.Post("/users", s.requireAdmin(s.handleCreateUser))
			r.Put("/users/{uid}", s.requireAdmin(s.handleUpdateUser))
			r.Delete("/users/{uid}", s.requireAdmin(s.handleDeleteUser))

			// 设置
			r.Get("/settings", s.requireAdmin(s.handleGetSettings))
			r.Put("/settings", s.requireAdmin(s.handlePutSettings))
			r.Get("/settings/binary", s.handleBinaryInfo)
			r.Post("/settings/binary/check", s.handleBinaryCheck)
			r.Post("/settings/binary/download", s.requireAdmin(s.handleBinaryDownload))
			r.Post("/settings/schema/refresh", s.requireAdmin(s.handleSchemaRefresh))

			// 全局审计
			r.Get("/audit", s.requireAdmin(s.handleAuditList))
		})
	})

	// SPA 回退：未知路径给 index.html（在 chi 中通过 404 hook 处理）
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		staticFS.ServeHTTP(w, r)
	})
	return r
}

// requireAdmin 包一层权限校验。
func (s *Server) requireAdmin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.CurrentUser(r)
		if u == nil || u.Role != "admin" {
			writeErr(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		h(w, r)
	}
}

// realIP 采信反代头：仅当直连端来自回环/内网时，才用 X-Real-IP /
// X-Forwarded-For 改写 RemoteAddr。公网直连时这些头可任意伪造，
// 采信它们会让登录限速形同虚设；裸反代（不传头）场景不受影响。
// 注：公网 IP 的云负载均衡不在“可信网络”内，此时所有限速共用一个桶——
// 自托管典型部署（同机 nginx / docker 内网）不受影响。
func realIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
			if rip := firstForwardedIP(r); rip != "" {
				if pip := net.ParseIP(rip); pip != nil {
					r.RemoteAddr = net.JoinHostPort(rip, "0")
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func firstForwardedIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		return strings.TrimSpace(strings.SplitN(v, ",", 2)[0])
	}
	return ""
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				writeErr(w, http.StatusInternalServerError, "内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- 通用工具 ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func readBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

func pathParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

func queryInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
