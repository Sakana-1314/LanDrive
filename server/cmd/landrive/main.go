// 局域网文件助手 —— 局域网共享网盘服务端。
//
// 单一二进制，内嵌 Vue 前端产物；启动流程：
// 读取配置 → 打开数据库并迁移 → 初始化数据根目录 → 播种管理员与默认配置
// → 启动维护任务 → 监听 HTTP。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"lan-drive/internal/auth"
	"lan-drive/internal/config"
	"lan-drive/internal/files"
	"lan-drive/internal/handler"
	"lan-drive/internal/maintain"
	"lan-drive/internal/model"
	"lan-drive/internal/router"
	"lan-drive/internal/settings"
	"lan-drive/internal/storage"
	"lan-drive/internal/store"
	"lan-drive/internal/upload"
)

// version 由构建参数注入（-ldflags "-X main.version=..."）。
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "打印版本并退出")
	flag.Parse()
	if *showVersion {
		fmt.Println("局域网文件助手", version)
		return
	}

	setupLogger()
	if err := run(); err != nil {
		slog.Error("启动失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.Info("局域网文件助手 启动中", "version", version, "config", cfg.String())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 数据库：连接 + 自动建库 + 迁移（内置 60s 重试，适配容器启动顺序）。
	st, err := store.Open(ctx, cfg.MySQLDSN, cfg.AutoCreateDB)
	if err != nil {
		return err
	}
	defer st.Close()

	// 数据根目录与存储层。
	disk, err := storage.New(cfg.DataDir)
	if err != nil {
		return err
	}
	slog.Info("数据根目录已就绪", "path", disk.Root())

	// 默认配置播种 + 载入配置缓存。
	if err := settings.Seed(ctx, st); err != nil {
		return fmt.Errorf("写入默认配置失败: %w", err)
	}
	set, err := settings.New(ctx, st)
	if err != nil {
		return fmt.Errorf("载入系统配置失败: %w", err)
	}

	// 管理员播种（仅当 users 表为空时）。
	if err := seedAdmin(ctx, st, disk, cfg); err != nil {
		return err
	}

	tokens := auth.NewTokenManager(cfg.JWTSecret, auth.TokenTTL)
	filesSvc := files.New(st, disk, set)
	// 把 files 的展示字段逻辑注入上传服务，避免包循环依赖。
	uploadsSvc := upload.New(st, disk, set, filesSvc.Decorate)
	maintSvc := maintain.New(st, disk, set)

	h := handler.New(handler.Deps{
		Store:    st,
		Storage:  disk,
		Settings: set,
		Files:    filesSvc,
		Uploads:  uploadsSvc,
		Maintain: maintSvc,
		Tokens:   tokens,
	})

	// 维护任务调度。
	go schedule(ctx, maintSvc, set)

	engine := router.New(h, cfg)
	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           engine,
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
		// 不设置 WriteTimeout：大文件下载与视频拖动可能持续很久。
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("内网 API 服务已启动", "listen", cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("收到退出信号，正在优雅关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("优雅关闭超时", "error", err)
	}
	return nil
}

// seedAdmin 在 users 表为空时创建初始管理员；否则跳过。
func seedAdmin(ctx context.Context, st *store.Store, disk *storage.Storage, cfg *config.Config) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("统计账号失败: %w", err)
	}
	if n > 0 {
		slog.Info("账号已存在，跳过初始管理员创建", "users", n)
		return nil
	}
	if strings.TrimSpace(cfg.AdminPassword) == "" {
		return errors.New("首次启动需要设置 LANDRIVE_ADMIN_PASSWORD 以创建初始管理员账号")
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("初始管理员密码不合法: %w", err)
	}
	u := &model.User{
		EmployeeNo: cfg.AdminEmpNo,
		Name:       cfg.AdminName,
		Password:   hash,
		Role:       model.RoleAdmin,
		Enabled:    true,
	}
	if err := st.CreateUser(ctx, u); err != nil {
		return fmt.Errorf("创建初始管理员失败: %w", err)
	}
	dirRel, err := disk.EnsureUserDir(u.EmployeeNo)
	if err != nil {
		return fmt.Errorf("创建初始管理员目录失败: %w", err)
	}
	if err := st.SetUserDirRel(ctx, u.ID, dirRel); err != nil {
		return fmt.Errorf("初始化初始管理员目录失败: %w", err)
	}
	slog.Info("已创建初始管理员", "employee_no", u.EmployeeNo, "name", u.Name, "dir", dirRel)
	return nil
}

// schedule 启动各维护任务的定时器，并在退出信号后停止。
func schedule(ctx context.Context, m *maintain.Service, set *settings.Service) {
	// 启动 30 秒后先跑一轮，避免和启动初始化抢资源。
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		m.RunExpire(ctx)
		m.RunPurge(ctx)
		m.RunCleanStaleUploads(ctx)
	}()

	tickers := []struct {
		name     string
		interval time.Duration
		fn       func(context.Context)
	}{
		{"到期标记", 5 * time.Minute, func(c context.Context) { m.RunExpire(c) }},
		{"物理清理", 10 * time.Minute, func(c context.Context) { m.RunPurge(c) }},
		{"僵尸上传清理", time.Hour, func(c context.Context) { m.RunCleanStaleUploads(c) }},
	}
	for _, t := range tickers {
		go runTicker(ctx, t.name, t.interval, t.fn)
	}
	// 每日 03:30（UTC）执行日志裁剪与孤儿扫描。
	go runDaily(ctx, m)
}

func runTicker(ctx context.Context, name string, interval time.Duration, fn func(context.Context)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// 每个任务用独立的超时上下文，避免单个任务卡死影响其它任务。
			tctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			slog.Debug("执行维护任务", "task", name)
			fn(tctx)
			cancel()
		}
	}
}

func runDaily(ctx context.Context, m *maintain.Service) {
	for {
		next := nextDailyAt(time.Now().UTC(), 3, 30)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			tctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			m.RunDaily(tctx)
			cancel()
		}
	}
}

// nextDailyAt 返回当前时间之后最近的一个 hour:min（UTC）。
func nextDailyAt(now time.Time, hour, min int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// setupLogger 配置结构化日志。
func setupLogger() {
	level := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LANDRIVE_LOG_LEVEL"))) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}
