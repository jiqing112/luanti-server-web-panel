// Package luantibin: luantiserver 二进制的获取与版本管理。
//
// 分层策略：
//  1. 手动路径（设置 luanti_binary_path 指向已有二进制）
//  2. 自动下载预编译二进制（ROllerozxa/luantiserver GitHub Releases，
//     匹配 x86_64/aarch64，解压后放入 data/bin/luantiserver）
//  3. 下载失败时 UI 引导手动上传/填路径
package luantibin

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultRepo = "ROllerozxa/luantiserver"
	UserAgent   = "LuantiWebPanel/1.0"
)

type Manager struct {
	DataDir string
	HTTP    *http.Client
}

func New(dataDir string) *Manager {
	return &Manager{
		DataDir: dataDir,
		HTTP:    &http.Client{Timeout: 15 * time.Minute},
	}
}

func (m *Manager) BinDir() string   { return filepath.Join(m.DataDir, "bin") }
func (m *Manager) BinaryPath() string {
	name := "luantiserver"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(m.BinDir(), name)
}
func (m *Manager) ExampleConfPath() string { return filepath.Join(m.BinDir(), "minetest.conf.example") }

// ---- 版本 ----

var reVersion = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+)\b`)

// ProbeVersion 运行二进制 --version，解析版本号。
func (m *Manager) ProbeVersion(path string) (string, error) {
	if path == "" {
		path = m.BinaryPath()
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("二进制不存在: %s", path)
	}
	if st.IsDir() {
		return "", fmt.Errorf("路径是目录而非文件: %s", path)
	}
	out, err := runCmd(path)
	if err != nil {
		return "", fmt.Errorf("运行 --version 失败: %v", err)
	}
	if mv := reVersion.FindStringSubmatch(out); mv != nil {
		return mv[1], nil
	}
	return "", fmt.Errorf("无法从输出解析版本: %q", firstLine(out))
}

// ReleaseAsset 描述发布资产（直接取下载地址，避免二次请求）。
type ReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ReleaseInfo 描述远端最新版本。
type ReleaseInfo struct {
	Tag     string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// LatestRelease 查询 GitHub Releases 最新版本。
func (m *Manager) LatestRelease(repo string) (*ReleaseInfo, error) {
	if repo == "" {
		repo = DefaultRepo
	}
	req, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo), nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}
	var r ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// archKey 把 GOARCH 映射为发布资产的架构关键字。
func archKey() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("不支持的架构: %s（可手动放置二进制路径）", runtime.GOARCH)
	}
}

// pickAsset 从资产中选出非 debug 的常规压缩包（tar.gz 优先，zip 兑底）。
func pickAsset(assets []ReleaseAsset, arch string) (ReleaseAsset, bool) {
	var fallback ReleaseAsset
	for _, a := range assets {
		if a.Name == "" || !strings.Contains(a.Name, arch) {
			continue
		}
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, "debug") {
			continue
		}
		if strings.HasSuffix(lower, ".tar.gz") {
			return a, true
		}
		if strings.HasSuffix(lower, ".zip") && fallback.Name == "" {
			fallback = a
		}
	}
	return fallback, fallback.Name != ""
}

// Download 下载并安装最新版二进制到 data/bin/。返回版本号。
func (m *Manager) Download(repo string) (string, error) {
	if repo == "" {
		repo = DefaultRepo
	}
	rel, err := m.LatestRelease(repo)
	if err != nil {
		return "", fmt.Errorf("查询最新版本失败: %w", err)
	}
	arch, err := archKey()
	if err != nil {
		return "", err
	}

	// 单次 API 调用同时取 tag 与资产列表（旧实现重复请求 releases/latest，
	// 既浪费 GitHub 匿名限额，也不检查第二次请求的状态码）
	asset, ok := pickAsset(rel.Assets, arch)
	if !ok {
		return "", fmt.Errorf("发布 %s 中没有 %s 架构的资产", rel.Tag, arch)
	}

	dlURL := asset.URL
	if dlURL == "" {
		dlURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, rel.Tag, asset.Name)
	}
	tmpDir, err := os.MkdirTemp("", "luantibin-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	req, _ := http.NewRequest("GET", dlURL, nil)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载返回 %d: %s", resp.StatusCode, dlURL)
	}
	archivePath := filepath.Join(tmpDir, asset.Name)
	f, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		return "", err
	}

	// 解压到临时目录
	extractDir := filepath.Join(tmpDir, "x")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", err
	}
	if strings.HasSuffix(asset.Name, ".tar.gz") {
		if err := extractTarGz(archivePath, extractDir); err != nil {
			return "", err
		}
	} else {
		if err := extractZipArchive(archivePath, extractDir); err != nil {
			return "", err
		}
	}

	// 定位二进制（任意深度，排除 debug 变体）
	binPath, err := findBinary(extractDir)
	if err != nil {
		return "", err
	}
	// 运行时根目录：二进制所在目录；目录名为 bin 时取其父目录
	shareRoot := filepath.Dir(binPath)
	if strings.EqualFold(filepath.Base(shareRoot), "bin") {
		shareRoot = filepath.Dir(shareRoot)
	}

	// 把运行时根目录的全部内容（builtin/、minetest.conf.example 等）
	// 移入 data/bin —— 该目录将作为每个实例的 share 根
	entries, err := os.ReadDir(shareRoot)
	if err != nil {
		return "", err
	}
	_ = os.MkdirAll(m.BinDir(), 0o755)
	for _, e := range entries {
		if strings.EqualFold(e.Name(), "bin") {
			continue // 归档自带的 bin/ 只含二进制本身，最后单独处理
		}
		src := filepath.Join(shareRoot, e.Name())
		dst := filepath.Join(m.BinDir(), e.Name())
		_ = os.RemoveAll(dst)
		if err := os.Rename(src, dst); err != nil {
			if err := copyDir(src, dst); err != nil {
				return "", err
			}
		}
	}

	// 二进制就位（binPath 在 shareRoot/bin 内，未被上面移动）
	finalBin := m.BinaryPath()
	_ = os.Remove(finalBin)
	if err := os.Rename(binPath, finalBin); err != nil {
		if err := copyExecutable(binPath, finalBin); err != nil {
			return "", err
		}
	}
	_ = os.Chmod(finalBin, 0o755)
	return strings.TrimPrefix(rel.Tag, "v"), nil
}

// findBinary 在目录中任意深度查找服务端二进制（排除 debug 变体）。
func findBinary(dir string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, e error) error {
		if e != nil || !info.Mode().IsRegular() {
			return nil
		}
		name := strings.ToLower(info.Name())
		if (strings.HasPrefix(name, "luantiserver") || strings.HasPrefix(name, "minetestserver")) &&
			!strings.Contains(name, "debug") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("压缩包内未找到 luantiserver 二进制")
	}
	return found, nil
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func extractTarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		if strings.Contains(name, "..") {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, tr)
			out.Close()
			if err != nil {
				return err
			}
		}
	}
}

func extractZipArchive(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, "..") || name == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		_ = os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// FetchExampleConf 从 GitHub 拉取 minetest.conf.example（压缩包不含时兜底）。
func (m *Manager) FetchExampleConf() error {
	req, _ := http.NewRequest("GET",
		"https://api.github.com/repos/luanti-org/luanti/contents/minetest.conf.example", nil)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github.raw")
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}
	_ = os.MkdirAll(m.BinDir(), 0o755)
	return os.WriteFile(m.ExampleConfPath(), streamAll(resp.Body), 0o644)
}

func streamAll(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
