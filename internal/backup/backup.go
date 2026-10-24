// Package backup: 世界 zip 备份/恢复 + 配置历史 + 定时调度。
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"luantipanel/internal/luanti"
	"luantipanel/internal/notify"
	"luantipanel/internal/store"
)

// Manifest 备份元数据（与 zip 同目录的 <name>.json）。
type Manifest struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Size      int64  `json:"size"`
	SizeLabel string `json:"size_label,omitempty"`
	Locked    bool   `json:"locked"`
	Kind      string `json:"kind"` // world | config
}

func manifestPath(zipPath string) string { return zipPath + ".json" }

// CreateWorldBackup 打包 worlds/<worldname>/ 到 backups/world/。
func CreateWorldBackup(d luanti.Dirs, worldname string) (*Manifest, error) {
	src := d.World(worldname)
	if _, err := os.Stat(filepath.Join(src, "world.mt")); err != nil {
		return nil, fmt.Errorf("世界目录无效（缺少 world.mt）")
	}
	name := fmt.Sprintf("world-%s.zip", time.Now().Format("20060102-150405"))
	dst := filepath.Join(d.BackupsWorld(), name)
	if err := os.MkdirAll(d.BackupsWorld(), 0o755); err != nil {
		return nil, err
	}
	size, err := zipDir(src, dst)
	if err != nil {
		return nil, err
	}
	m := &Manifest{Name: name, CreatedAt: time.Now().Format(time.RFC3339), Size: size, Kind: "world"}
	_ = writeManifest(filepath.Join(d.BackupsWorld(), name+".json"), m)
	return m, nil
}

// RestoreWorldBackup 用备份覆盖世界目录（调用方需先停止实例）。
func RestoreWorldBackup(d luanti.Dirs, worldname, backupName string) error {
	zipPath := filepath.Join(d.BackupsWorld(), backupName)
	if !safeName(backupName) {
		return fmt.Errorf("非法备份名")
	}
	if _, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("备份不存在")
	}
	tmpDir, err := os.MkdirTemp(filepath.Join(d.Root, "backups"), "restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := unzip(zipPath, tmpDir); err != nil {
		return fmt.Errorf("解压失败: %w", err)
	}
	// 兼容两种结构：直接是 world.mt 或包在单层目录里
	root := tmpDir
	if _, err := os.Stat(filepath.Join(tmpDir, "world.mt")); err != nil {
		entries, _ := os.ReadDir(tmpDir)
		if len(entries) == 1 && entries[0].IsDir() {
			root = filepath.Join(tmpDir, entries[0].Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "world.mt")); err != nil {
		return fmt.Errorf("备份内容不是有效世界（缺少 world.mt）")
	}

	worldDir := d.World(worldname)
	oldDir := worldDir + ".old-" + time.Now().Format("20060102150405")
	if err := os.Rename(worldDir, oldDir); err != nil {
		return err
	}
	if err := os.Rename(root, worldDir); err != nil {
		_ = os.Rename(oldDir, worldDir) // 回滚
		return err
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

// ListWorldBackups 列出备份（读取 manifest，缺失时从文件信息生成）。
func ListWorldBackups(d luanti.Dirs) []*Manifest {
	return listByKind(d.BackupsWorld(), "world", ".zip")
}

func listByKind(dir, kind, ext string) []*Manifest {
	out := []*Manifest{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		full := filepath.Join(dir, e.Name())
		var m Manifest
		if b, err := os.ReadFile(manifestPath(full)); err == nil && json.Unmarshal(b, &m) == nil && m.Name == e.Name() {
			m.SizeLabel = humanSize(m.Size)
			out = append(out, &m)
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, &Manifest{
			Name:      e.Name(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			Size:      info.Size(),
			SizeLabel: humanSize(info.Size()),
			Kind:      kind,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(1024*1024*1024))
	}
}

func (m *Manifest) SetLock(d luanti.Dirs, locked bool) error {
	m.Locked = locked
	return writeManifest(filepath.Join(d.BackupsWorld(), m.Name+".json"), m)
}

// DeleteWorldBackup 删除备份（锁定除外，由调用方检查）。
func DeleteWorldBackup(d luanti.Dirs, name string) error {
	if !safeName(name) {
		return fmt.Errorf("非法备份名")
	}
	_ = os.Remove(filepath.Join(d.BackupsWorld(), name+".json"))
	return os.Remove(filepath.Join(d.BackupsWorld(), name))
}

func safeName(n string) bool {
	if n == "" || strings.ContainsAny(n, "/\\") || strings.Contains(n, "..") {
		return false
	}
	return true
}

// ---- 配置历史 ----

// SnapshotConf 保存配置前先快照当前 conf。
func SnapshotConf(d luanti.Dirs) (string, error) {
	if _, err := os.Stat(d.Config()); err != nil {
		return "", nil
	}
	if err := os.MkdirAll(d.BackupsConfig(), 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("conf-%s.conf", time.Now().Format("20060102-150405"))
	dst := filepath.Join(d.BackupsConfig(), name)
	b, err := os.ReadFile(d.Config())
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func ListConfigHistory(d luanti.Dirs) []*Manifest {
	return listByKind(d.BackupsConfig(), "config", ".conf")
}

func ConfigHistoryPath(d luanti.Dirs, name string) (string, error) {
	if !safeName(name) {
		return "", fmt.Errorf("非法文件名")
	}
	p := filepath.Join(d.BackupsConfig(), name)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("历史版本不存在")
	}
	return p, nil
}

// ---- 定时调度 ----

type Scheduler struct {
	st       *store.Store
	dataDir  string
	notify   *notify.Notifier
	cleanups int

	// SaveHook 备份前回调：面板用它让运行中的实例先经 bridge 执行 /save，
	// 降低 SQLite 世界文件被拷到撕裂状态的概率。nil 时跳过。
	SaveHook func(instID string)

	mu       sync.Mutex
	lastRuns map[string]time.Time // instanceID → 上次自动备份时间
}

func NewScheduler(st *store.Store, dataDir string, n *notify.Notifier) *Scheduler {
	return &Scheduler{st: st, dataDir: dataDir, notify: n, lastRuns: map[string]time.Time{}}
}

// Run 每分钟检查：到期自动备份、数据清理。
func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tick()
		}
	}
}

func (s *Scheduler) tick() {
	insts, err := s.st.ListInstances()
	if err != nil {
		return
	}
	for _, inst := range insts {
		if inst.BackupIntervalHours <= 0 {
			continue
		}
		d := luanti.Dirs{Root: filepath.Join(s.dataDir, "instances", inst.ID)}
		if _, err := os.Stat(d.World(inst.WorldName)); err != nil {
			continue
		}
		s.mu.Lock()
		last := s.lastRuns[inst.ID]
		s.mu.Unlock()
		if last.IsZero() {
			// 面板刚启动：以最新备份时间为基准
			if list := ListWorldBackups(d); len(list) > 0 {
				if t, err := time.Parse(time.RFC3339, list[0].CreatedAt); err == nil {
					last = t
				}
			}
		}
		if !last.IsZero() && time.Since(last) < time.Duration(inst.BackupIntervalHours)*time.Hour {
			continue
		}
		s.mu.Lock()
		s.lastRuns[inst.ID] = time.Now()
		s.mu.Unlock()
		if s.SaveHook != nil {
			s.SaveHook(inst.ID)
		}
		if _, err := CreateWorldBackup(d, inst.WorldName); err == nil {
			rotate(d.BackupsWorld(), inst.BackupKeepCount, ".zip")
		} else {
			fmt.Printf("[scheduler] 实例 %s 自动备份失败: %v\n", inst.Name, err)
		}
	}

	// 每小时级清理：借 ticker 次数取模
	s.cleanups++
	if s.cleanups%60 == 1 {
		_ = s.st.CleanSessions()
		_ = s.st.PruneAudit(10000)
		_ = s.st.PruneActivityByDays(90)
	}
}

// RotateDir 保留最近 keep 份（ext 指定文件扩展名，如 ".zip" / ".conf"）。
// 旧实现硬编码 ".zip"，导致配置快照（.conf）永远不被轮转、无限累积。
func RotateDir(dir string, keep int, ext string) { rotate(dir, keep, ext) }

// rotate 轮转：保留最近 keep 份未锁定的备份。
func rotate(dir string, keep int, ext string) {
	if keep <= 0 {
		keep = 10
	}
	list := listByKind(dir, "world", ext)
	kept := 0
	for _, m := range list {
		if m.Locked {
			continue
		}
		kept++
		if kept > keep {
			_ = os.Remove(filepath.Join(dir, m.Name))
			_ = os.Remove(filepath.Join(dir, m.Name+".json"))
		}
	}
}

// ---- zip 工具 ----

func zipDir(srcDir, dstZip string) (int64, error) {
	f, err := os.Create(dstZip)
	if err != nil {
		return 0, err
	}
	w := zip.NewWriter(f)

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr := &zip.FileHeader{Name: filepath.ToSlash(rel), Modified: info.ModTime()}
		if info.IsDir() {
			hdr.Name += "/"
			_, err := w.CreateHeader(hdr)
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		hdr.SetMode(info.Mode())
		fw, err := w.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(fw, src)
		return err
	})
	if err != nil {
		f.Close()
		return 0, err
	}
	// 必须先落盘关闭再取大小
	if err := w.Close(); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	info, err := os.Stat(dstZip)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func unzip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("非法路径: %s", f.Name)
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
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

func writeManifest(p string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
