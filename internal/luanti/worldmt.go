package luanti

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---- world.mt 安全读写 ----
//
// world.mt 是 key = value 格式，可能含注释行。修改时保留原有行与注释，
// 精确替换目标键；load_mod_* 按官方 world_format.md 支持两种值：
//
//	load_mod_x = true            — 在搜索路径中自动发现
//	load_mod_x = mods/x          — 相对 user path 的路径（本面板统一使用）
type WorldMT struct {
	path   string
	lines  []string
	values map[string]string
}

// LoadWorldMT 读取 world.mt；文件不存在时返回带 world_name/gameid 基础键的新实例。
func LoadWorldMT(path string) (*WorldMT, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &WorldMT{path: path, values: map[string]string{}}, nil
		}
		return nil, err
	}
	w := &WorldMT{path: path, values: map[string]string{}}
	for _, raw := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		w.lines = append(w.lines, raw)
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok {
			k = strings.TrimSpace(k)
			w.values[k] = strings.TrimSpace(v)
		}
	}
	return w, nil
}

func (w *WorldMT) Get(key string) string { return w.values[key] }

// Set 设置键值：原位替换已有行，否则追加。
func (w *WorldMT) Set(key, value string) {
	w.values[key] = value
	for i, raw := range w.lines {
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			w.lines[i] = fmt.Sprintf("%s = %s", key, value)
			return
		}
	}
	w.lines = append(w.lines, fmt.Sprintf("%s = %s", key, value))
}

func (w *WorldMT) Delete(key string) {
	delete(w.values, key)
	for i, raw := range w.lines {
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			w.lines = append(w.lines[:i], w.lines[i+1:]...)
			return
		}
	}
}

// SetLoadMod 写入 load_mod_<name> = mods/<name>（或 false 禁用）。
func (w *WorldMT) SetLoadMod(modName string, enabled bool) {
	if enabled {
		w.Set("load_mod_"+modName, "mods/"+modName)
	} else {
		w.Set("load_mod_"+modName, "false")
	}
}

func (w *WorldMT) LoadMods() map[string]string {
	out := map[string]string{}
	for k, v := range w.values {
		if strings.HasPrefix(k, "load_mod_") {
			out[strings.TrimPrefix(k, "load_mod_")] = v
		}
	}
	return out
}

// Render 输出 world.mt 文本。
func (w *WorldMT) Render() string {
	return strings.Join(w.lines, "\n") + "\n"
}

// Save 原子写入。
func (w *WorldMT) Save() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(w.Render()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, w.path)
}

// BaseWorldMT 生成新世界的基础 world.mt。
func BaseWorldMT(worldName, gameID string) string {
	var b strings.Builder
	b.WriteString("# 本文件由 Luanti Web Panel 生成\n")
	b.WriteString(fmt.Sprintf("world_name = %s\n", worldName))
	b.WriteString(fmt.Sprintf("gameid = %s\n", gameID))
	b.WriteString("backend = sqlite3\n")
	b.WriteString("player_backend = sqlite3\n")
	b.WriteString("auth_backend = sqlite3\n")
	b.WriteString("mod_storage_backend = sqlite3\n")
	return b.String()
}
