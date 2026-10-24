// Package notify: 崩溃/故障告警（Discord/Slack/通用 webhook）。
package notify

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Notifier struct {
	mu        sync.Mutex
	url       string // 设置中的 webhook 地址
	kind      string // discord | slack | generic
	client    *http.Client
	lastSent  map[string]time.Time // 每实例告警冷却
	cooldown  time.Duration
}

func New() *Notifier {
	return &Notifier{
		client:   &http.Client{Timeout: 10 * time.Second},
		lastSent: map[string]time.Time{},
		cooldown: 10 * time.Minute,
	}
}

func (n *Notifier) Configure(url, kind string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.url = strings.TrimSpace(url)
	if kind == "" {
		kind = "discord"
	}
	n.kind = kind
}

func (n *Notifier) Enabled() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.url != ""
}

// Send 发送一条告警（带每实例冷却，避免风暴）。
func (n *Notifier) Send(key, title, detail string) {
	n.mu.Lock()
	url := n.url
	kind := n.kind
	if url == "" {
		n.mu.Unlock()
		return
	}
	if t, ok := n.lastSent[key]; ok && time.Since(t) < n.cooldown {
		n.mu.Unlock()
		return
	}
	n.lastSent[key] = time.Now()
	n.mu.Unlock()

	text := "**" + title + "**\n" + detail
	var payload any
	switch kind {
	case "slack":
		payload = map[string]string{"text": text}
	default: // discord 与大多数通用 webhook 兼容 content
		payload = map[string]string{"content": text}
	}
	b, _ := json.Marshal(payload)
	resp, err := n.client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		log.Printf("[notify] webhook 发送失败: %v", err)
		return
	}
	resp.Body.Close()
}
