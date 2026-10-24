package luanti

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ---- minetest.conf 读写 ----

type ConfLine struct {
	Raw     string
	Key     string
	Value   string
	Comment bool // 整行注释（# key = value 形式的示例行）
}

// ParseConf 逐行解析 minetest.conf。返回行列表与有效键值。
// 非法行（既不是注释/空行，也不是 key = value）会报错。
func ParseConf(content string) ([]ConfLine, map[string]string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	out := make([]ConfLine, 0, len(lines))
	kv := map[string]string{}
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		switch {
		case t == "" || strings.HasPrefix(t, "#"):
			if t != "" && strings.HasPrefix(t, "#") && strings.TrimSpace(strings.TrimPrefix(t, "#")) != "" {
				rest := strings.TrimSpace(strings.TrimPrefix(t, "#"))
				if k, v, ok := strings.Cut(rest, "="); ok {
					k = strings.TrimSpace(k)
					if isConfKey(k) {
						// 示例行（# key = value）：记录但不生效
						out = append(out, ConfLine{Raw: raw, Key: k, Value: strings.TrimSpace(v), Comment: true})
						continue
					}
				}
			}
			out = append(out, ConfLine{Raw: raw})
		default:
			k, v, ok := strings.Cut(t, "=")
			if !ok || !isConfKey(strings.TrimSpace(k)) {
				return nil, nil, fmt.Errorf("第 %d 行不是合法的配置行: %q（合法格式为 key = value 或以 # 开头的注释）", i+1, raw)
			}
			key := strings.TrimSpace(k)
			out = append(out, ConfLine{Raw: raw, Key: key, Value: strings.TrimSpace(v)})
			kv[key] = strings.TrimSpace(v)
		}
	}
	return out, kv, nil
}

func isConfKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}

// MergeValues 把要修改的键合并进 conf 文本：已有键原位替换，其余追加到
// 「面板托管」段。保留注释与其余行不动。
func MergeValues(content string, values map[string]string) (string, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	pending := map[string]string{}
	for k, v := range values {
		pending[k] = v
	}
	managedIdx := -1
	for i, raw := range lines {
		t := strings.TrimSpace(raw)
		if managedIdx < 0 && t == "# === 面板托管设置 ===" {
			managedIdx = i
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		k, _, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if v, need := pending[k]; need {
			lines[i] = fmt.Sprintf("%s = %s", k, v)
			delete(pending, k)
		}
	}
	if len(pending) == 0 {
		return strings.Join(lines, "\n"), nil
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines, "\n"))
	if managedIdx < 0 {
		b.WriteString("\n\n# === 面板托管设置 ===\n# 由 Web 面板追加，勿手动删除\n")
	}
	for k, v := range pending {
		b.WriteString(fmt.Sprintf("%s = %s\n", k, v))
	}
	return b.String(), nil
}

// ValidateValue 按类型校验取值（bool/int/float/enum/string/key）。
func ValidateValue(typ, val string, enums []string) error {
	switch typ {
	case "bool":
		v := strings.ToLower(val)
		if v != "true" && v != "false" && v != "yes" && v != "no" {
			return fmt.Errorf("布尔值应为 true/false")
		}
	case "int":
		if _, err := strconv.Atoi(strings.TrimSpace(val)); err != nil {
			return fmt.Errorf("应为整数")
		}
	case "float":
		if _, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err != nil {
			return fmt.Errorf("应为数字")
		}
	case "enum":
		if len(enums) > 0 {
			ok := false
			for _, e := range enums {
				if strings.EqualFold(e, val) {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("可选值: %s", strings.Join(enums, ", "))
			}
		}
	case "key", "string", "v3f", "v2f", "v3s16", "rectf", "":
		// 自由文本，但禁止换行
		if strings.ContainsAny(val, "\r\n") {
			return fmt.Errorf("不允许换行")
		}
	default:
		if strings.ContainsAny(val, "\r\n") {
			return fmt.Errorf("不允许换行")
		}
	}
	return nil
}

// ReadConfFile 读配置文件。
func ReadConfFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteConfFile 原子写配置文件。
func WriteConfFile(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// BaseConf 生成新实例的基础配置。
func BaseConf(port int, adminName, bridgeURL, bridgeToken, mgName, seed string) string {
	var b strings.Builder
	b.WriteString("# 本文件由 Luanti Web Panel 生成，可在面板中编辑\n")
	b.WriteString("# 官方完整选项请参考面板「配置」页的表单\n\n")
	b.WriteString("# === 服务器基础 ===\n")
	b.WriteString(fmt.Sprintf("port = %d\n", port))
	b.WriteString("# 管理员玩家名（拥有全部权限，面板控制台以其身份执行命令）\n")
	b.WriteString(fmt.Sprintf("name = %s\n", adminName))
	b.WriteString("\n# === 面板桥接 ===\n")
	b.WriteString("# 面板控制台/在线玩家心跳依赖 panel_bridge mod（世界目录 worldmods/ 下自动加载）\n")
	b.WriteString("secure.http_mods = panel_bridge\n")
	b.WriteString(fmt.Sprintf("panel_bridge.url = %s\n", bridgeURL))
	b.WriteString(fmt.Sprintf("panel_bridge.token = %s\n", bridgeToken))
	if mgName != "" && mgName != "auto" {
		b.WriteString("\n# === 地形生成 ===\n")
		b.WriteString(fmt.Sprintf("mg_name = %s\n", mgName))
	}
	if seed != "" {
		b.WriteString("# 固定地图种子（留空则每次随机）\n")
		b.WriteString(fmt.Sprintf("fixed_map_seed = %s\n", seed))
	}
	return b.String()
}
