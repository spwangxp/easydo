package main

import (
	"easydo-server/internal/config"
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

	// 初始化数据库
	models.InitDB()

	// 初始化 Redis
	utils.InitRedis()

	// 启动Agent离线检测定时任务
	cronService := services.GetCronService(models.DB)
	cronService.StartAgentOfflineChecker()
	defer cronService.StopAgentOfflineChecker()

	// 初始化路由
	router := routers.InitRouter()

	// 启动服务
	router.Run(":8080")
}
