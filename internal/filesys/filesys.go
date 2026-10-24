// Package filesys: 实例文件浏览（路径逃逸防护 + 文本判定 + 读写限制）。
package filesys

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	MaxEditSize   = 2 * 1024 * 1024  // 在线编辑上限 2MB
	MaxUploadSize = 100 * 1024 * 1024 // 上传上限 100MB
)

// SafeJoin 把相对路径限制在 root 内（含符号链接逃逸检查）。
func SafeJoin(root, rel string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if rel == "" || rel == "." {
		return absRoot, nil
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径不允许离开实例目录")
	}
	target := filepath.Join(absRoot, clean)

	// 符号链接逃逸检查：解析已存在的最深祖先
	cur := target
	var toResolve []string
	for {
		if _, err := os.Lstat(cur); err != nil {
			// 不存在，向上继续
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			toResolve = append(toResolve, cur)
			cur = parent
			continue
		}
		break
	}
	resolved := cur
	if r, err := filepath.EvalSymlinks(cur); err == nil {
		resolved = r
	} else {
		resolved = cur
	}
	_ = toResolve
	if !withinRoot(absRoot, resolved) {
		return "", fmt.Errorf("路径不允许离开实例目录（符号链接）")
	}
	return target, nil
}

func withinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

type Entry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	Text    bool   `json:"text"` // 是否可在线编辑
}

// ListDir 列目录（目录在前）。
func ListDir(root, rel string) ([]*Entry, error) {
	dir, err := SafeJoin(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("不是目录")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, e.Name())
		isText := false
		if !e.IsDir() && fi.Size() <= MaxEditSize {
			isText = IsTextFile(e.Name())
		}
		_ = p
		out = append(out, &Entry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    fi.Size(),
			ModTime: fi.ModTime().Format("2006-01-02 15:04"),
			Text:    isText,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

var textExts = map[string]bool{
	".conf": true, ".txt": true, ".lua": true, ".mt": true, ".json": true,
	".md": true, ".csv": true, ".log": true, ".sh": true, ".yml": true,
	".yaml": true, ".ini": true, ".example": true, ".cfg": true, ".tsv": true,
	".sql": true, ".js": true, ".css": true, ".html": true, ".xml": true,
	".gradle": true, ".properties": true, ".toml": true, ".env": true,
	"game.conf": true, "mod.conf": true,
}

// IsTextFile 按扩展名判定是否可在线编辑；无扩展名时放行少量已知文件名。
func IsTextFile(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	if base == "game.conf" || base == "mod.conf" || base == "world.mt" ||
		base == "license" || base == "readme" || base == "changelog" || base == "makefile" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	return textExts[ext]
}

// ReadText 读取文本文件（限大小）。
func ReadText(root, rel string) (string, error) {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if fi.IsDir() {
		return "", fmt.Errorf("是目录")
	}
	if fi.Size() > MaxEditSize {
		return "", fmt.Errorf("文件超过 2MB，请下载后编辑")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if !utf8ish(b) {
		return "", fmt.Errorf("文件不是 UTF-8 文本，请下载后用本地编辑器处理")
	}
	return string(b), nil
}

func utf8ish(b []byte) bool {
	// 粗略判定：前 8KB 内不允许出现 0x00；非 UTF-8 字节序列不做严格校验
	n := len(b)
	if n > 8192 {
		n = 8192
	}
	for _, c := range b[:n] {
		if c == 0 {
			return false
		}
	}
	return true
}

// WriteText 写文本文件（原子写）。
func WriteText(root, rel, content string) error {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	if len(content) > MaxEditSize {
		return fmt.Errorf("内容超过 2MB 上限")
	}
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("文件不存在，如需新建请使用上传")
	}
	tmp := p + ".panel-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Upload 保存上传内容。
func Upload(root, rel string, r io.Reader, limit int64) error {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	out, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(r, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		_ = os.Remove(p)
		return fmt.Errorf("超过大小限制")
	}
	return nil
}

// Delete 删除文件或目录。
func Delete(root, rel string) error {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	if p == root {
		return fmt.Errorf("不能删除实例根目录")
	}
	return os.RemoveAll(p)
}

// MkdirAll 创建目录。
func MkdirAll(root, rel string) error {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

// Open 打开文件供下载。
func Open(root, rel string) (*os.File, os.FileInfo, error) {
	p, err := SafeJoin(root, rel)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, nil, err
	}
	if fi.IsDir() {
		return nil, nil, fmt.Errorf("是目录")
	}
	f, err := os.Open(p)
	return f, fi, err
}
