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
	"fmt"
)

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

	// 启动服务
	router.Run(":8080")
}
