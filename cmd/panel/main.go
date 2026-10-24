// Luanti Web Panel — 入口。
// 用法: panel -addr :8080 -data ./data
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"luantipanel/internal/api"
	"luantipanel/internal/backup"
	"luantipanel/internal/store"
)

//go:embed all:version.txt
var versionFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "面板监听地址")
	dataFlag := flag.String("data", "data", "数据目录")
	flag.Parse()

	dataDir, err := filepath.Abs(*dataFlag)
	if err != nil {
		log.Fatalf("数据目录无效: %v", err)
	}
	for _, d := range []string{dataDir, filepath.Join(dataDir, "instances"), filepath.Join(dataDir, "bin"), filepath.Join(dataDir, "games"), filepath.Join(dataDir, "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatalf("创建目录失败 %s: %v", d, err)
		}
	}

	st, err := store.Open(filepath.Join(dataDir, "panel.db"))
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	if err := st.InitAdmin(); err != nil {
		log.Fatalf("初始化管理员失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := api.New(api.Options{
		Store:   st,
		DataDir: dataDir,
		Version: readVersion(),
		Addr:    *addr,
	})

	// 备份调度 + 数据清理
	scheduler := backup.NewScheduler(st, dataDir, srv.Notify)
	// 定时备份前先让运行中的实例 /save（bridge 优雅存盘）
	scheduler.SaveHook = func(id string) {
		if srv.RT.StatusOf(id) == "running" {
			_, _ = srv.RT.Command(id, "/save")
		}
	}
	go scheduler.Run(ctx)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("监听失败: %v", err)
	}

	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Println("正在关闭面板...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		srv.Shutdown() // 并行停止全部子进程（优雅存盘），冲刷活动日志
		close(shutdownDone)
	}()

	fmt.Printf("Luanti Web Panel %s 已启动: http://localhost%s  (数据目录: %s)\n", srv.Version, displayAddr(*addr), dataDir)
	if pw := st.InitialAdminPassword(); pw != "" {
		fmt.Printf("首次运行：默认管理员账号 admin，初始密码: %s\n", pw)
		pwFile := filepath.Join(dataDir, "initial_admin_password.txt")
		_ = os.WriteFile(pwFile, []byte(
			"admin: "+pw+"\n（此文件由面板首次启动生成，修改密码后请删除）\n"), 0o600)
		fmt.Printf("（也已写入 %s，登录后请立即在「设置→修改密码」中更改）\n", pwFile)
	}
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("HTTP 服务异常: %v", err)
	}
	// 等待关闭流程完成（子进程优雅停止 + 活动日志冲刷）再退出，
	// 避免主进程提前退出导致正在存盘的服务端被截断（Linux 下甚至成为孤儿进程）
	<-shutdownDone
	log.Println("面板已退出")
}

func displayAddr(a string) string {
	if _, p, err := net.SplitHostPort(a); err == nil {
		return ":" + p
	}
	return a
}

func readVersion() string {
	b, err := versionFS.ReadFile("version.txt")
	if err != nil || len(b) == 0 {
		return "dev"
	}
	v, err := strconv.Unquote(string(b))
	if err != nil {
		return "dev"
	}
	return v
}
