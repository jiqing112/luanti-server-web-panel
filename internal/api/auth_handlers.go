package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"luantipanel/internal/auth"
	"luantipanel/internal/store"
)

// ---- 登录 / 会话 ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readBody(r, &body); err != nil || body.Username == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "请提供用户名与密码")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	user, err := s.Auth.Login(ip, body.Username, body.Password)
	if err != nil {
		var rl *auth.RateLimitError
		if errors.As(err, &rl) {
			w.Header().Set("Retry-After", "60")
			writeErr(w, http.StatusTooManyRequests, err.Error())
			return
		}
		writeErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token := store.RandomToken(32)
	_ = s.st.CreateSession(token, user.ID, auth.SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})
	s.st.Audit(user.Username, "login", "", "登录面板")
	writeJSON(w, 200, user)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.Auth.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	if u == nil {
		writeErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	writeJSON(w, 200, u)
}

// ---- 用户管理 ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readBody(r, &body); err != nil || body.Username == "" || body.Password == "" {
		writeErr(w, 400, "请提供用户名与密码")
		return
	}
	if len(body.Password) < 6 {
		writeErr(w, 400, "密码至少 6 位")
		return
	}
	if body.Role != "admin" && body.Role != "operator" {
		body.Role = "operator"
	}
	body.Username = strings.TrimSpace(body.Username)
	if len(body.Username) < 2 || len(body.Username) > 32 {
		writeErr(w, 400, "用户名长度需在 2-32 之间")
		return
	}
	hash, err := store.HashPassword(body.Password)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := s.st.CreateUser(body.Username, hash, body.Role); err != nil {
		writeErr(w, 500, "创建失败（用户名可能已存在）")
		return
	}
	s.st.Audit(u.Username, "user.create", "", "创建用户 "+body.Username+" ("+body.Role+")")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	uid := queryInt(r, "", 0)
	if v := pathParam(r, "uid"); v != "" {
		uid = queryIntFrom(v)
	}
	var body struct {
		Password *string `json:"password"`
		Role     *string `json:"role"`
	}
	if err := readBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.Password != nil && *body.Password != "" {
		if len(*body.Password) < 6 {
			writeErr(w, 400, "密码至少 6 位")
			return
		}
		hash, err := store.HashPassword(*body.Password)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if err := s.st.UpdateUserPassword(int64(uid), hash); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		s.st.Audit(u.Username, "user.password", "", "重置用户密码 id="+pathParam(r, "uid"))
	}
	if body.Role != nil {
		if *body.Role != "admin" && *body.Role != "operator" {
			writeErr(w, 400, "角色只能是 admin 或 operator")
			return
		}
		if int64(uid) == u.ID && *body.Role != "admin" {
			writeErr(w, 400, "不能降级自己的角色")
			return
		}
		if err := s.st.UpdateUserRole(int64(uid), *body.Role); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		s.st.Audit(u.Username, "user.role", "", "修改用户角色 id="+pathParam(r, "uid")+" → "+*body.Role)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	u := auth.CurrentUser(r)
	uid := queryIntFrom(pathParam(r, "uid"))
	if int64(uid) == u.ID {
		writeErr(w, 400, "不能删除自己")
		return
	}
	if err := s.st.DeleteUser(int64(uid)); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.st.Audit(u.Username, "user.delete", "", "删除用户 id="+pathParam(r, "uid"))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func queryIntFrom(v string) int {
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
