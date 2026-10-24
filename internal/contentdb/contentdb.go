// Package contentdb: ContentDB (content.luanti.org) API 客户端与安装器。
package contentdb

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL   = "https://content.luanti.org"
	UserAgent = "LuantiWebPanel/1.0"
)

type cacheEntry struct {
	data    json.RawMessage
	expires time.Time
}

type Client struct {
	HTTP  *http.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

const (
	ttlSearch  = 10 * time.Minute // 搜索结果变化较频繁
	ttlPackage = 30 * time.Minute // 包详情/截图变化慢
	ttlDeps    = 10 * time.Minute
)

func New() *Client {
	// 下载大体积游戏包（80MB+）在慢速网络下可能耗时数分钟
	return &Client{
		HTTP:  &http.Client{Timeout: 15 * time.Minute},
		cache: map[string]cacheEntry{},
	}
}

// cachedRaw 带内存缓存的 GET。上游失败时回退过期缓存（stale-if-error），
// 在网络抖动或 ContentDB 限流时保持页面可用，并显著降低请求频率。
func (c *Client) cachedRaw(path string, ttl time.Duration) (json.RawMessage, error) {
	c.mu.Lock()
	e, ok := c.cache[path]
	c.mu.Unlock()
	if ok && time.Now().Before(e.expires) {
		return e.data, nil
	}
	raw, err := c.fetchRaw(path)
	if err != nil {
		if ok {
			return e.data, nil
		}
		return nil, err
	}
	c.mu.Lock()
	if len(c.cache) > 600 {
		now := time.Now()
		for k, v := range c.cache {
			if now.After(v.expires) {
				delete(c.cache, k)
			}
		}
	}
	c.cache[path] = cacheEntry{data: raw, expires: time.Now().Add(ttl)}
	c.mu.Unlock()
	return raw, nil
}

func (c *Client) fetchRaw(path string) (json.RawMessage, error) {
	req, err := http.NewRequest("GET", BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ContentDB 返回 %d: %s", resp.StatusCode, path)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// getJSON 默认 TTL 的缓存 GET（搜索 / 依赖列表）。
func (c *Client) getJSON(path string, out any) error {
	raw, err := c.cachedRaw(path, ttlDeps)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Package 搜索结果条目（release 即最新发布 ID，可直接用于下载）。
type Package struct {
	Author           string `json:"author"`
	Name             string `json:"name"`
	Release          int64  `json:"release"`
	Title            string `json:"title"`
	ShortDescription string `json:"short_description"`
	Type             string `json:"type"`
	Thumbnail        string `json:"thumbnail"`
	Downloads        int64  `json:"downloads"`
	Score            float64 `json:"score"`
}

func (p Package) FullName() string { return p.Author + "/" + p.Name }

// Search 关键词搜索；typ 为 mod/game/txp，可空。
func (c *Client) Search(q, typ string, limit int) ([]Package, error) {
	v := url.Values{}
	if q != "" {
		v.Set("q", q)
	}
	if typ != "" {
		v.Set("type", typ)
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	v.Set("limit", fmt.Sprint(limit))
	var out []Package
	if err := c.getJSON("/api/packages/?"+v.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Dependency 依赖解析返回的条目：name 为 modname，packages 为可提供该
// 依赖的包（author/name）候选列表。
type Dependency struct {
	Name       string   `json:"name"`
	IsOptional bool     `json:"is_optional"`
	Packages   []string `json:"packages"`
}

// GetPackage 获取单个包详情（含最新 release id）。
func (c *Client) GetPackage(author, name string) (*Package, error) {
	raw, err := c.cachedRaw(fmt.Sprintf("/api/packages/%s/%s/",
		url.PathEscape(author), url.PathEscape(name)), ttlPackage)
	if err != nil {
		return nil, err
	}
	var out Package
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRaw 获取 JSON 原始内容（透传未知字段，供详情模态框使用）。
func (c *Client) GetRaw(path string) (json.RawMessage, error) {
	return c.cachedRaw(path, ttlPackage)
}

// Dependencies 获取包的依赖（?only_hard=1 只含硬依赖）。
// 返回以 author/name 为键的整棵依赖树。
func (c *Client) Dependencies(author, name string) (map[string][]Dependency, error) {
	var out map[string][]Dependency
	err := c.getJSON(fmt.Sprintf("/api/packages/%s/%s/dependencies/?only_hard=1",
		url.PathEscape(author), url.PathEscape(name)), &out)
	return out, err
}

// ResolveDeps 递归解析硬依赖，返回需要安装的包列表（依赖优先、根包最后）。
// have: 判断某 modname 是否已满足（游戏自带或已安装）。
func (c *Client) ResolveDeps(rootAuthor, rootName string, have func(string) bool) ([]Package, error) {
	var order []Package
	seenName := map[string]bool{}   // 已满足/已排队的 modname
	queuedPkg := map[string]bool{}  // 已排队的 author/name

	satisfy := func(modname string) bool {
		if have != nil && have(modname) {
			return true
		}
		return seenName[modname]
	}

	var visit func(author, name string) error
	visit = func(author, name string) error {
		key := author + "/" + name
		if queuedPkg[key] {
			return nil
		}
		queuedPkg[key] = true

		tree, err := c.Dependencies(author, name)
		if err != nil {
			return fmt.Errorf("获取 %s 依赖失败: %w", key, err)
		}
		deps := tree[key]
		for _, d := range deps {
			if d.IsOptional || satisfy(d.Name) {
				continue
			}
			if len(d.Packages) == 0 {
				return fmt.Errorf("依赖 %s 没有可安装的包", d.Name)
			}
			// 优先选择与 modname 同名的包，否则取候选列表第一个
			chosen := d.Packages[0]
			for _, p := range d.Packages {
				if strings.HasSuffix(p, "/"+d.Name) {
					chosen = p
					break
				}
			}
			pa, pn, _ := strings.Cut(chosen, "/")
			if err := visit(pa, pn); err != nil {
				return err
			}
		}
		// 自身入队（依赖已先处理）
		var pkg Package
		if err := c.getJSON(fmt.Sprintf("/api/packages/%s/%s/", url.PathEscape(author), url.PathEscape(name)), &pkg); err != nil {
			return err
		}
		order = append(order, pkg)
		seenName[name] = true
		return nil
	}

	if err := visit(rootAuthor, rootName); err != nil {
		return nil, err
	}
	return order, nil
}

// InstallZip 下载包 zip 并解压到 destDir（mods/ 或 games/）。
// zip 内是单个根目录（ContentDB 规范），解压时剥离；返回落地的目录名。
func (c *Client) InstallZip(pkg Package, destDir string) (string, error) {
	dl := fmt.Sprintf("%s/packages/%s/%s/releases/%d/download/",
		BaseURL, url.PathEscape(pkg.Author), url.PathEscape(pkg.Name), pkg.Release)
	req, err := http.NewRequest("GET", dl, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载返回 %d: %s", resp.StatusCode, dl)
	}

	tmpZip, err := os.CreateTemp("", "cdb-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpZip.Name())
	// 流式限额：超限立即中止（旧实现先完整落盘再检查，恶意/异常响应可填满磁盘）
	const maxPkgSize = 512 << 20
	size, err := io.Copy(tmpZip, io.LimitReader(resp.Body, maxPkgSize+1))
	tmpZip.Close()
	if err != nil {
		return "", err
	}
	if size > maxPkgSize {
		return "", fmt.Errorf("包体积超过 512MB 上限")
	}

	name, err := extractZip(tmpZip.Name(), destDir, pkg.Name)
	if err != nil {
		return "", err
	}
	return name, nil
}

// extractZip 解压 zip 到 destDir。ContentDB 包的 zip 带单一根目录（即包目录），
// 保持其结构解压；若无根目录（文件平铺），则解压到 destDir/<fallbackName> 子目录。
// 返回实际落地的目录名。防御 zip-slip。
func extractZip(zipPath, destDir, fallbackName string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("解压失败: %w", err)
	}
	defer r.Close()
	if len(r.File) == 0 {
		return "", fmt.Errorf("压缩包为空")
	}

	// 判定是否所有条目都在同一根目录下
	root := ""
	all := true
	for _, f := range r.File {
		p := filepath.ToSlash(f.Name)
		first, _, _ := strings.Cut(p, "/")
		if first == "" || first == p {
			all = false
			break
		}
		if root == "" {
			root = first
		} else if first != root {
			all = false
			break
		}
	}

	target := destDir
	if !all {
		root = ""
	}
	if root == "" && fallbackName != "" {
		if !safeName(fallbackName) {
			return "", fmt.Errorf("包名不合法: %s", fallbackName)
		}
		target = filepath.Join(destDir, fallbackName)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue
		}
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			return "", fmt.Errorf("压缩包内含非法路径: %s", f.Name)
		}
		dst := filepath.Join(target, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return "", err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return "", err
		}
	}
	if !all {
		return fallbackName, nil
	}
	return root, nil
}

func safeName(n string) bool {
	if n == "" || strings.ContainsAny(n, "/\\") || strings.Contains(n, "..") {
		return false
	}
	return true
}
