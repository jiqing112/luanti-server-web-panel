// Package luanti: 实例目录布局、minetest.conf 与 world.mt 读写、schema 解析。
package luanti

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dirs 描述一个实例的磁盘布局。
// RUN_IN_PLACE 约定：二进制必须位于 <root>/bin/，引擎以 exe 目录的上一级
// 作为 share/user 路径（src/porting.cpp: path_share = execpath + "/.."），
// 因此 games/ mods/ worlds/ 都放在实例根目录。
type Dirs struct {
	Root string
}

func (d Dirs) Bin() string            { return filepath.Join(d.Root, "bin") }
func (d Dirs) Binary() string         { return filepath.Join(d.Bin(), BinaryName()) }
func (d Dirs) ConfigDir() string      { return filepath.Join(d.Root, "config") }
func (d Dirs) Config() string         { return filepath.Join(d.ConfigDir(), "minetest.conf") }
func (d Dirs) Games() string          { return filepath.Join(d.Root, "games") }
func (d Dirs) Mods() string           { return filepath.Join(d.Root, "mods") }
func (d Dirs) Worlds() string         { return filepath.Join(d.Root, "worlds") }
func (d Dirs) World(name string) string { return filepath.Join(d.Worlds(), name) }
func (d Dirs) Logs() string           { return filepath.Join(d.Root, "logs") }
func (d Dirs) LogFile() string        { return filepath.Join(d.Logs(), "debug.txt") }
func (d Dirs) BackupsWorld() string   { return filepath.Join(d.Root, "backups", "world") }
func (d Dirs) BackupsConfig() string  { return filepath.Join(d.Root, "backups", "config") }

// BinaryName 返回当前平台的服务端二进制名。
func BinaryName() string {
	if isWindows() {
		return "luantiserver.exe"
	}
	return "luantiserver"
}

func isWindows() bool { return os.PathSeparator == '\\' }

// EnsureLayout 创建实例目录结构。
func (d Dirs) EnsureLayout(worldname string) error {
	for _, p := range []string{d.Root, d.Bin(), d.ConfigDir(), d.Games(), d.Mods(), d.Worlds(), d.Logs(), d.BackupsWorld(), d.BackupsConfig()} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s: %w", p, err)
		}
	}
	if worldname != "" {
		if err := os.MkdirAll(d.World(worldname), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// PlaceBinary 把面板共享二进制放入实例 bin/ 目录。
// 优先硬链接（/proc/self/exe 会解析符号链接，故不能用 symlink），失败则复制。
// 共享二进制更新后（大小或新旧时间变化）强制刷新实例副本：
// 旧实现“已存在即跳过”会让老实例永远用旧版，而 builtin 运行时每次启动都会
// 重新镜像，出现“新 builtin + 旧二进制”的版本错配。
func (d Dirs) PlaceBinary(masterPath string) error {
	dst := d.Binary()
	st, err := os.Stat(masterPath)
	if err != nil {
		return fmt.Errorf("共享二进制不存在: %s（请先在设置页下载或指定路径）", masterPath)
	}
	if cur, err := os.Stat(dst); err == nil && cur.Size() > 0 &&
		cur.Size() == st.Size() && !st.ModTime().After(cur.ModTime()) {
		return nil // 已就位且与共享二进制一致
	}
	_ = os.Remove(dst)
	if err := os.Link(masterPath, dst); err != nil {
		// 跨文件系统等情况，退回复制
		if err := copyFile(masterPath, dst, st.Mode()); err != nil {
			return err
		}
	}
	_ = os.Chmod(dst, 0o755)
	return nil
}

// runtimeMirrorDirs 是随二进制分发的引擎运行时目录（builtin 必需）。
var runtimeMirrorDirs = []string{"builtin", "textures", "fonts", "client"}

// MirrorRuntime 把共享目录里的引擎运行时（builtin/ 等）镜像到实例根目录。
// RUN_IN_PLACE 布局下引擎从 share 根读取 builtin/，缺少则无法启动。
// 每次实例启动时调用；镜像始终覆盖（builtin 属于引擎文件，非用户数据）。
func MirrorRuntime(masterBinDir, instanceRoot string) error {
	for _, name := range runtimeMirrorDirs {
		src := filepath.Join(masterBinDir, name)
		st, err := os.Stat(src)
		if err != nil || !st.IsDir() {
			continue
		}
		dst := filepath.Join(instanceRoot, name)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		if err := mirrorTree(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func mirrorTree(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, p)
		if err != nil {
			return nil
		}
		dst := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		_ = os.Remove(dst)
		// 硬链接优先（同盘零拷贝），失败退回复制
		if err := os.Link(p, dst); err == nil {
			return nil
		}
		return copyFile(p, dst, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// UdpPortFree 尝试绑定 UDP 端口检测占用。
func UdpPortFree(port int) bool {
	conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Game 表示一个已安装游戏（games/<id>/ 内含 game.conf）。
type Game struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

// ListGames 扫描目录下的游戏。
func ListGames(dir string) ([]Game, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Game
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		gp := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(gp, "game.conf")); err != nil {
			continue
		}
		g := Game{ID: e.Name(), Path: gp}
		if kv, err := ParseKVFile(filepath.Join(gp, "game.conf")); err == nil {
			g.Title = kv["name"]
		}
		if g.Title == "" {
			g.Title = e.Name()
		}
		out = append(out, g)
	}
	return out, nil
}

// ---- key = value 解析（game.conf / mod.conf / world.mt / minetest.conf 共用）----

// ParseKV 解析 key = value 文本；忽略注释与空行。
func ParseKV(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k != "" {
				out[k] = v
			}
		}
	}
	return out
}

func ParseKVFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseKV(string(b)), nil
}

// IsValidPlayerName 与引擎 PLAYERNAME_ALLOWED_CHARS 对齐。
func IsValidPlayerName(s string) bool {
	if len(s) < 2 || len(s) > 20 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// WaitForFile 等待文件出现（用于轮询 bridge 握手等场景）。
func WaitForFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}
