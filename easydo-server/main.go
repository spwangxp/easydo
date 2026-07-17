package main

import (
	"context"
	"easydo-server/internal/config"
	"easydo-server/internal/handlers"
	"easydo-server/internal/migrations"
	"easydo-server/internal/models"
	"easydo-server/internal/routers"
	"easydo-server/internal/services"
	"easydo-server/pkg/utils"
	"easydo-server/pkg/version"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type realtimeShutdowner interface {
	Shutdown(context.Context) error
}

func main() {
	// Print version information (single line for container logs)
	fmt.Println("[INFO] " + version.Info())
	fmt.Println()

	// 初始化配置
	config.Init()
	if err := config.ValidateMultiReplicaRequirements(); err != nil {
		panic(err)
	}
	// 服务启动时先执行内置的版本化 SQL 迁移，确保所有副本共享同一套迁移历史与锁语义。
	if err := migrations.RunEmbeddedFromConfig(context.Background()); err != nil {
		panic("Failed to apply database migrations: " + err.Error())
	}

	// 初始化数据库
	models.InitDB()

	// 初始化 Redis
	utils.InitRedis()

	// 启动Agent离线检测定时任务
	cronService := services.GetCronService(models.DB)
	cronService.StartAgentOfflineChecker()
	cronService.StartNotificationDeliveryProcessor()
	cronService.StartPipelineTriggerEvaluator(handlers.NewPipelineHandler())
	defer cronService.StopAgentOfflineChecker()
	queuedRunScheduler := handlers.NewQueuedRunScheduler(models.DB, 0)
	queuedRunScheduler.Start()
	defer queuedRunScheduler.Stop()

	// 初始化路由
	router := routers.InitRouter()

	addr := fmt.Sprintf(":%d", config.Config.GetInt("server.port"))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		panic("Failed to listen on " + addr + ": " + err.Error())
	}

	serverCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: addr, Handler: router}
	if err := serveHTTPWithGracefulShutdown(serverCtx, server, listener, handlers.SharedWebSocketHandler(), 15*time.Second); err != nil {
		panic("HTTP server stopped with error: " + err.Error())
	}
}

func serveHTTPWithGracefulShutdown(ctx context.Context, server *http.Server, listener net.Listener, realtime realtimeShutdowner, shutdownTimeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if server == nil {
		return fmt.Errorf("http server is nil")
	}
	if listener == nil {
		return fmt.Errorf("http listener is nil")
	}
	if shutdownTimeout <= 0 {
		shutdownTimeout = 15 * time.Second
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	var shutdownErr error
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if realtime != nil {
		shutdownErr = errors.Join(shutdownErr, realtime.Shutdown(shutdownCtx))
	}

	select {
	case err := <-errCh:
		shutdownErr = errors.Join(shutdownErr, err)
	case <-shutdownCtx.Done():
		shutdownErr = errors.Join(shutdownErr, shutdownCtx.Err())
	}
	return shutdownErr
}
