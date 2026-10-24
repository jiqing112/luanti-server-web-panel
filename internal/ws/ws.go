// Package ws: 面向单个实例的 WebSocket 连接 hub，负责日志/玩家/指标/状态广播。
package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     sameOrigin,
}

// sameOrigin 仅允许同源（Cookie 认证下防 CSRF 的 WS 部分）。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // 非浏览器客户端
	}
	host := r.Host
	u, err := parseOriginHost(origin)
	if err != nil {
		return false
	}
	return u == host
}

func parseOriginHost(origin string) (string, error) {
	// 简化解析：scheme://host[:port]
	s := origin
	for _, p := range []string{"https://", "http://", "ws://", "wss://"} {
		if len(s) > len(p) && s[:len(p)] == p {
			s = s[len(p):]
			break
		}
	}
	for i, c := range s {
		if c == '/' {
			return s[:i], nil
		}
	}
	return s, nil
}

type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: map[*client]struct{}{}}
}

func (h *Hub) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

// Broadcast 向所有订阅者广播；发送缓冲满则丢弃该消息（日志流不能阻塞生产者）。
func (h *Hub) Broadcast(msgType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	env, err := json.Marshal(Message{Type: msgType, Data: data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- env:
		default:
		}
	}
}

const (
	pingInterval = 30 * time.Second // 服务端主动 ping，防中间设备掐断空闲连接
	pongWait     = 90 * time.Second // 超过该时长未收到任何数据/pong 则判定连接已死
)

// Serve 升级连接并接管读写；auth 返回 false 时拒绝升级。
// onHello 在客户端注册后调用一次，用于发送初始快照；
// onCommand 处理客户端发来的 {"type":"command","text":...}。
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, auth func() bool,
	onCommand func(text string), onHello func(send func(string, any)), onClose func()) {
	if !auth() {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 512)}
	h.add(c)

	send := func(msgType string, payload any) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		env, err := json.Marshal(Message{Type: msgType, Data: data})
		if err != nil {
			return
		}
		select {
		case c.send <- env:
		default:
		}
	}
	if onHello != nil {
		onHello(send)
	}

	done := make(chan struct{})
	go func() { // writer
		defer func() { h.remove(c); conn.Close(); if onClose != nil { onClose() } }()
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case msg := <-c.send:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// reader：仅处理命令与保活
	go func() {
		defer func() { close(done); h.remove(c); conn.Close() }()
		conn.SetReadLimit(64 * 1024)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(pongWait))
		})
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			var m Message
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			if m.Type == "command" && onCommand != nil {
				var p struct{ Text string `json:"text"` }
				if json.Unmarshal(m.Data, &p) == nil && p.Text != "" {
					onCommand(p.Text)
				}
			}
		}
	}()
}

