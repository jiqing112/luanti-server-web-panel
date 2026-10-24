package luanti

import (
	"strings"

	"luantipanel/internal/store"
)

// ParseExampleConf 解析 minetest.conf.example（官方完整选项列表）。
//
// 文件结构（实际样本）：
//
//	#    描述文字……
//	#    type: float min: 0 max: 100
//	# camera_smoothing = 0.0
//
// 小节标题两种形式：`#\n# Controls\n#\n` 与 `## General`。
// type 行可带 min/max/enum 约束。
func ParseExampleConf(content string) []store.SchemaRow {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	var rows []store.SchemaRow
	var desc []string
	var typ string
	var enums []string
	var section, header string

	flush := func(key, def string) {
		if key == "" {
			return
		}
		t := typ
		if t == "" {
			t = "string"
		}
		rows = append(rows, store.SchemaRow{
			Key:         key,
			Type:        t,
			Default:     def,
			Description: strings.Join(desc, " "),
			Category:    categorize(section, header, key),
			EnumValues:  enums,
		})
		desc = nil
		typ = ""
		enums = nil
	}
	resetBuf := func() { desc = nil; typ = ""; enums = nil }

	for _, raw := range lines {
		t := strings.TrimSpace(raw)
		if t == "" || t == "#" {
			continue
		}
		if strings.HasPrefix(t, "## ") { // 小节标题（双井号形式）
			section = strings.TrimSpace(strings.TrimPrefix(t, "##"))
			header = section
			resetBuf()
			continue
		}
		if !strings.HasPrefix(t, "#") {
			// 未注释的实际配置行 key = value
			if k, v, ok := strings.Cut(t, "="); ok {
				k = strings.TrimSpace(k)
				if isSettingKey(k) {
					flush(k, strings.TrimSpace(v))
				}
			}
			resetBuf()
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if body == "" {
			continue
		}
		if strings.HasPrefix(body, "type:") {
			typ, enums = parseTypeLine(body)
			continue
		}
		if looksLikeHeader(body) {
			header = body
			section = body
			resetBuf()
			continue
		}
		if k, v, ok := strings.Cut(body, "="); ok {
			k = strings.TrimSpace(k)
			if isSettingKey(k) { // 设置示例行 # key = value
				flush(k, strings.TrimSpace(v))
				continue
			}
		}
		desc = append(desc, body)
	}
	return rows
}

// isSettingKey 排除形如 "0 = disable" 的描述行：真实配置键必含字母。
func isSettingKey(k string) bool {
	if !isConfKey(k) {
		return false
	}
	for _, r := range k {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

func looksLikeHeader(body string) bool {
	// 标题行特征：较短、无等号、不是 key 样式
	if strings.Contains(body, "=") || len(body) > 40 {
		return false
	}
	if strings.HasPrefix(body, "*") {
		return false
	}
	if !strings.Contains(body, " ") && (strings.Contains(body, "_") || strings.ToLower(body) == body) {
		return false // camera_smoothing 这类是 key 不是标题
	}
	return true
}

func parseTypeLine(body string) (string, []string) {
	rest := strings.TrimSpace(strings.TrimPrefix(body, "type:"))
	var enums []string
	if idx := strings.Index(rest, "values:"); idx >= 0 {
		vals := strings.TrimSpace(rest[idx+len("values:"):])
		for _, v := range strings.Split(vals, ",") {
			if v = strings.TrimSpace(strings.Trim(v, `"'`)); v != "" {
				enums = append(enums, v)
			}
		}
		rest = strings.TrimSpace(rest[:idx])
	}
	fields := strings.Fields(rest)
	if len(fields) > 0 {
		return fields[0], enums
	}
	return "string", enums
}

var sectionCategory = map[string]string{
	"General":             "服务器基础",
	"Networking":          "网络",
	"Server":              "服务器基础",
	"Security":            "安全",
	"Mapgen":              "游戏性",
	"Chat":                "游戏性",
	"Physics":             "游戏性",
	"Player":              "游戏性",
	"Liquid":              "游戏性",
	"World":               "游戏性",
	"Status":              "服务器基础",
	"Controls":            "客户端",
	"Graphics":            "客户端",
	"Graphics: Effects":   "客户端",
	"Graphics: Filtering": "客户端",
	"Sound":               "客户端",
	"Keybinds":            "客户端",
	"Pause menu":          "客户端",
	"Singleplayer":        "客户端",
	"Client":              "客户端",
	"Development":         "高级",
	"Advanced":            "高级",
	"Engine":              "高级",
}

var keyCategoryRules = []struct {
	sub string
	cat string
}{
	{"port", "网络"}, {"bind", "网络"}, {"ipv6", "网络"}, {"protocol", "网络"},
	{"bandwidth", "网络"}, {"connection", "网络"}, {"lag", "网络"}, {"timeout", "网络"},
	{"ssl", "安全"}, {"secure", "安全"}, {"password", "安全"}, {"priv", "安全"},
	{"ban", "安全"}, {"disallow", "安全"},
	{"max_users", "服务器基础"}, {"motd", "服务器基础"}, {"server_", "服务器基础"},
	{"default_game", "服务器基础"}, {"announce", "服务器基础"},
	{"active_block", "性能"}, {"max_block", "性能"}, {"emerge", "性能"},
	{"cache", "性能"}, {"threads", "性能"}, {"profiler", "高级"}, {"debug", "高级"},
	{"creative", "游戏性"}, {"damage", "游戏性"}, {"give", "游戏性"},
	{"mapgen", "游戏性"}, {"world", "游戏性"},
}

func categorize(section, header, key string) string {
	for _, s := range []string{section, header} {
		if s == "" {
			continue
		}
		if c, ok := sectionCategory[s]; ok {
			return c
		}
	}
	for _, r := range keyCategoryRules {
		if strings.Contains(key, r.sub) {
			return r.cat
		}
	}
	return "其他"
}
