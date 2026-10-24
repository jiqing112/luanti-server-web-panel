// Package auth: 会话认证（bcrypt + Cookie session）与角色中间件。
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"luantipanel/internal/store"
)

const CookieName = "panel_session"

type ctxKey int

const userKey ctxKey = 1

const SessionTTL = 7 * 24 * time.Hour

type Manager struct {
	st *store.Store

	rlMu     sync.Mutex
	failures map[string]*failInfo // ip → 记录
}

type failInfo struct {
	count int
	until time.Time // 锁定截止时间（零值表示未锁定）
	last  time.Time // 最近一次失败时间（过期条目回收用）
}

// dummyHash 用于用户名不存在时的耗时对齐（防用户名枚举：
// 让“用户不存在”与“密码错误”走同样长度的 bcrypt 计算）。
var dummyHash = func() string {
	h, _ := store.HashPassword("panel-timing-equalization-dummy")
	return h
}()

func New(st *store.Store) *Manager {
	return &Manager{st: st, failures: map[string]*failInfo{}}
}

// Login 校验用户名密码；带每 IP 限速（连续失败 5 次锁 1 分钟）。
// 会话令牌由 HTTP 层创建并落入 Cookie——保证一次登录只产生一条会话
// （旧实现 Login 内部与处理器各建一条，每次登录都会泄漏一条孤儿会话）。
func (m *Manager) Login(ip, username, password string) (*store.User, error) {
	now := time.Now()
	m.rlMu.Lock()
	// 回收过期条目（只增不减的 map 会随扫描流量无限增长）
	for k, fi := range m.failures {
		if (!fi.until.IsZero() && fi.until.Before(now)) || now.Sub(fi.last) > time.Hour {
			delete(m.failures, k)
		}
	}
	fi := m.failures[ip]
	if fi != nil && !fi.until.IsZero() && now.Before(fi.until) {
		remain := int(time.Until(fi.until).Seconds())
		m.rlMu.Unlock()
		return nil, &RateLimitError{RetryAfter: remain}
	}
	m.rlMu.Unlock()

	user, hash, err := m.st.GetUserByName(username)
	if err != nil {
		_ = store.CheckPassword(dummyHash, password) // 对齐耗时，防用户名枚举
		m.recordFailure(ip)
		return nil, errInvalidCredentials
	}
	if !store.CheckPassword(hash, password) {
		m.recordFailure(ip)
		return nil, errInvalidCredentials
	}

	m.rlMu.Lock()
	delete(m.failures, ip)
	m.rlMu.Unlock()

	return user, nil
}

// recordFailure 记录一次失败：满 5 次锁定该 IP 1 分钟。
func (m *Manager) recordFailure(ip string) {
	m.rlMu.Lock()
	defer m.rlMu.Unlock()
	if m.failures[ip] == nil {
		m.failures[ip] = &failInfo{}
	}
	fi := m.failures[ip]
	fi.last = time.Now()
	fi.count++
	if fi.count >= 5 {
		fi.until = time.Now().Add(time.Minute)
		fi.count = 0
	}
}

func (m *Manager) Logout(token string) { _ = m.st.DeleteSession(token) }

var errInvalidCredentials = &CredentialsError{}

type CredentialsError struct{}

func (*CredentialsError) Error() string { return "用户名或密码错误" }

type RateLimitError struct{ RetryAfter int }

func (e *RateLimitError) Error() string { return "尝试过于频繁，请稍后再试" }

// CurrentUser 从 context 取当前用户。
func CurrentUser(r *http.Request) *store.User {
	if u, ok := r.Context().Value(userKey).(*store.User); ok {
		return u
	}
	return nil
}

// LoadSession 中间件：解析 cookie → 用户，写入 context。
func (m *Manager) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
			if uid, err := m.st.GetSession(c.Value); err == nil {
				if u, err := m.st.GetUserByID(uid); err == nil {
					ctx := context.WithValue(r.Context(), userKey, u)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
		}
		// 未登录也要继续（由 RequireAuth 拦截），让登录页可访问
		next.ServeHTTP(w, r)
	})
}

// RequireAuth 要求已登录。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r) == nil {
			httpError(w, http.StatusUnauthorized, "未登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin 要求管理员。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := CurrentUser(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "未登录")
			return
		}
		if u.Role != "admin" {
			httpError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":` + jsonQuote(msg) + `}`))
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
