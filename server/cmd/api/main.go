package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"family-ledger/server/internal/config"
	"family-ledger/server/internal/httpapi"
	"family-ledger/server/internal/service"
	"family-ledger/server/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("启动失败", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.Env == "dev" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.Migrate(ctx); err != nil {
		return err
	}
	// 老家庭可能缺「通讯/保险」这类后加的分类，启动时补一次（幂等）
	if err := db.EnsureDefaultCategories(ctx, store.NowMS()); err != nil {
		return err
	}

	authSvc := service.NewAuthService(db, cfg)
	familySvc := service.NewFamilyService(db)
	ledgerSvc := service.NewLedgerService(db)
	adminSvc := service.NewAdminService(db, cfg)
	ledgerSvc.SetLLM(cfg)
	mailSvc := service.NewMailService(db, cfg, ledgerSvc)
	classifySvc := service.NewClassifyService(db, cfg)
	deviceSvc := service.NewDeviceService(db, cfg, authSvc)
	handler := httpapi.NewHandler(cfg, db, authSvc, familySvc, ledgerSvc, adminSvc, mailSvc, classifySvc, deviceSvc)

	// 账单邮箱定时拉取
	stopPolling := make(chan struct{})
	if cfg.MailPollMinutes > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.MailPollMinutes) * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					pollCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					mailSvc.SyncAll(pollCtx)
					cancel()
				case <-stopPolling:
					return
				}
			}
		}()
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewRouter(cfg, handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("服务已启动", "addr", cfg.Addr, "db", cfg.DBPath, "env", cfg.Env)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case serveErr := <-errCh:
		return serveErr
	case <-stopCtx.Done():
	}

	slog.Info("正在优雅关闭")
	close(stopPolling)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}
