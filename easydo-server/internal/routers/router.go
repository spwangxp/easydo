package routers

import (
	"easydo-server/internal/config"
	"easydo-server/internal/handlers"
	"easydo-server/internal/middleware"

	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {
	gin.SetMode(config.ServerMode())
	router := gin.Default()

	// 使用 CORS 中间件
	router.Use(middleware.CORSMiddleware())

	// Debug middleware - log all requests
	router.Use(func(c *gin.Context) {
		c.Next()
	})

	// 健康检查 - 在/api组下统一定义
	api := router.Group("/api")
	{
		api.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"code":    200,
				"message": "OK",
			})
		})

		// WebSocket endpoint for agent connections
		wsHandler := handlers.SharedWebSocketHandler()
		router.GET("/ws/agent/heartbeat", middleware.RateLimit(), wsHandler.HandleAgentConnection)

		// WebSocket endpoint for frontend real-time updates
		router.GET("/ws/frontend/pipeline", middleware.RateLimit(), wsHandler.HandleFrontendConnection)
		router.GET("/ws/frontend/terminal", middleware.RateLimit(), wsHandler.HandleTerminalFrontendConnection)

		// WebSocket endpoint for incoming proxy connections from remote servers (cross-server log forwarding)
		router.GET("/ws/proxy", wsHandler.HandleProxyConnection)
		router.POST("/api/pipeline/run/webhook/:token", middleware.RateLimit(), handlers.NewPipelineHandler().HandleGitLabWebhook)
		internal := router.Group("/internal")
		internal.Use(middleware.InternalServerAuth())
		internal.GET("/tasks/:id/live-logs", handlers.NewTaskHandler().GetTaskLiveLogsInternal)
		internal.GET("/store/chart-artifact", handlers.NewStoreTemplateHandler().DownloadResolvedChartInternal)

		// Debug endpoint - test raw body reading
		router.POST("/api/debug/body", func(c *gin.Context) {
			body, _ := c.GetRawData()
			c.JSON(200, gin.H{
				"received": string(body),
			})
		})

		// API 路由
		auth := api.Group("/auth")
		{
			userHandler := handlers.NewUserHandler()
			auth.POST("/login", middleware.RateLimit(), userHandler.Login)
			auth.POST("/register", middleware.RateLimit(), userHandler.Register)
			auth.POST("/refresh", middleware.JWTAuth(), userHandler.RefreshToken)
			auth.GET("/userinfo", middleware.JWTAuth(), middleware.WorkspaceContext(), userHandler.GetUserInfo)
			auth.PUT("/profile", middleware.JWTAuth(), userHandler.UpdateProfile)
			auth.PUT("/password", middleware.JWTAuth(), userHandler.ChangePassword)
			auth.POST("/logout", middleware.JWTAuth(), userHandler.Logout)
		}

		// 流水线相关
		pipeline := api.Group("/pipelines")
		pipeline.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			pipelineHandler := handlers.NewPipelineHandler()
			pipeline.GET("", pipelineHandler.GetPipelineList)
			pipeline.GET("/task-types", pipelineHandler.GetPipelineTaskTypes)
			pipeline.GET("/:id", pipelineHandler.GetPipelineDetail)
			pipeline.POST("", pipelineHandler.CreatePipeline)
			pipeline.PUT("/:id", pipelineHandler.UpdatePipeline)
			pipeline.DELETE("/:id", pipelineHandler.DeletePipeline)
			pipeline.POST("/:id/run", pipelineHandler.RunPipeline)
			pipeline.GET("/:id/triggers", pipelineHandler.GetPipelineTriggers)
			pipeline.PUT("/:id/triggers", pipelineHandler.UpdatePipelineTriggers)
			pipeline.GET("/:id/history", pipelineHandler.GetPipelineRuns)
			pipeline.GET("/:id/runs", pipelineHandler.GetPipelineRuns)
			pipeline.GET("/:id/runs/:run_id", pipelineHandler.GetRunDetail)
			pipeline.GET("/:id/runs/:run_id/tasks", pipelineHandler.GetRunTasks)
			pipeline.GET("/:id/runs/:run_id/logs", middleware.RateLimit(), pipelineHandler.GetRunLogs)
			pipeline.POST("/:id/runs/:run_id/cancel", pipelineHandler.CancelPipelineRun)
			pipeline.GET("/:id/statistics", middleware.RateLimit(), pipelineHandler.GetPipelineStatistics)
			pipeline.GET("/:id/test-reports", pipelineHandler.GetPipelineTestReports)
			pipeline.POST("/:id/favorite", pipelineHandler.ToggleFavorite)
		}

		// 项目相关
		project := api.Group("/projects")
		project.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			projectHandler := handlers.NewProjectHandler()
			project.GET("", projectHandler.GetProjectList)
			project.GET("/:id", projectHandler.GetProjectDetail)
			project.POST("", projectHandler.CreateProject)
			project.PUT("/:id", projectHandler.UpdateProject)
			project.DELETE("/:id", projectHandler.DeleteProject)
			project.POST("/:id/favorite", projectHandler.ToggleFavorite)
		}

		// 用户管理（仅管理员）
		users := api.Group("/users")
		users.Use(middleware.JWTAuth())
		{
			userHandler := handlers.NewUserHandler()
			users.POST("", userHandler.CreateUser)
			users.GET("", middleware.AdminRequired(), userHandler.GetUserList)
		}

		workspaces := api.Group("/workspaces")
		workspaces.Use(middleware.JWTAuth(), middleware.WorkspaceContext())
		{
			workspaceHandler := handlers.NewWorkspaceHandler()
			workspaces.GET("", workspaceHandler.GetWorkspaceList)
			workspaces.POST("", workspaceHandler.CreateWorkspace)
			workspaces.GET("/:id", workspaceHandler.GetWorkspace)
			workspaces.PATCH("/:id", workspaceHandler.UpdateWorkspace)
			workspaces.GET("/:id/members", workspaceHandler.ListMembers)
			workspaces.PATCH("/:id/members/:member_id", workspaceHandler.UpdateMember)
			workspaces.DELETE("/:id/members/:member_id", workspaceHandler.RemoveMember)
			workspaces.GET("/:id/invitations", workspaceHandler.ListInvitations)
			workspaces.POST("/:id/invitations", workspaceHandler.CreateInvitation)
			workspaces.DELETE("/:id/invitations/:invite_id", workspaceHandler.RevokeInvitation)
			workspaces.POST("/invitations/:token/accept", workspaceHandler.AcceptInvitation)
		}

		// Agent管理
		agents := api.Group("/agents")
		{
			agentHandler := handlers.NewAgentHandler()
			agents.POST("/register", agentHandler.RegisterAgent)                                                                                        // Agent注册，不需要认证
			agents.POST("/heartbeat", agentHandler.Heartbeat)                                                                                           // Agent心跳，不需要认证
			agents.POST("/select", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), agentHandler.SelectAgent) // 选择合适的Agent
			agents.POST("/self", agentHandler.GetAgentSelf)                                                                                             // Agent获取自己的信息（使用token验证）
			agents.GET("", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), agentHandler.GetAgentList)
			agents.GET("/:id", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), agentHandler.GetAgentDetail)
			agents.PUT("/:id", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.UpdateAgent)
			agents.DELETE("/:id", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.DeleteAgent)
			agents.GET("/:id/heartbeats", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), agentHandler.GetAgentHeartbeats)
			// Agent审批相关路由 - 需要管理员权限
			agents.GET("/pending", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.GetPendingAgents)
			agents.POST("/:id/approve", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.ApproveAgent)
			agents.POST("/:id/reject", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.RejectAgent)
			agents.POST("/:id/refresh-token", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.RefreshAgentToken)
			agents.POST("/:id/remove", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceRoleRequired("maintainer"), agentHandler.RemoveAgent)
		}

		messages := api.Group("/messages")
		messages.Use(middleware.JWTAuth())
		{
			notificationHandler := handlers.NewNotificationHandler()
			messages.GET("", notificationHandler.GetInbox)
			messages.GET("/unread-count", notificationHandler.GetUnreadInboxCount)
			messages.POST("/:id/read", notificationHandler.MarkInboxMessageRead)
			messages.POST("/read-all", notificationHandler.MarkAllInboxMessagesRead)
		}

		notifications := api.Group("/notifications")
		notifications.Use(middleware.JWTAuth())
		{
			notificationHandler := handlers.NewNotificationHandler()
			notifications.GET("/inbox", notificationHandler.GetInbox)
			notifications.GET("/inbox/unread-count", notificationHandler.GetUnreadInboxCount)
			notifications.POST("/inbox/:id/read", notificationHandler.MarkInboxMessageRead)
			notifications.POST("/inbox/read-all", notificationHandler.MarkAllInboxMessagesRead)
			notifications.GET("/preferences", notificationHandler.ListPreferences)
			notifications.PUT("/preferences", notificationHandler.UpsertPreference)
		}

		// Task管理
		tasks := api.Group("/tasks")
		{
			taskHandler := handlers.NewTaskHandler()
			tasks.POST("", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.CreateTask)
			tasks.GET("", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.GetTaskList)
			tasks.GET("/:id", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.GetTaskDetail)
			tasks.GET("/:id/logs", middleware.RateLimit(), middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.GetTaskLogs)
			tasks.POST("/:id/cancel", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.CancelTask)
			tasks.POST("/:id/retry", middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired(), taskHandler.RetryTask)
			// Agent上报接口（不需要认证，使用token验证）
			tasks.POST("/report/status", taskHandler.AgentReportTaskStatus)
			tasks.POST("/report/log", taskHandler.AgentReportLog)
			tasks.GET("/agent/:agent_id/pending", taskHandler.GetPendingTasks)
		}

		// 凭据管理 (Credentials)
		credentials := api.Group("/v1/credentials")
		credentials.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			credentialHandler := handlers.NewCredentialHandler()
			credentials.GET("", credentialHandler.ListCredentials)
			credentials.POST("", credentialHandler.CreateCredential)
			credentials.GET("/types", credentialHandler.GetCredentialTypes)
			credentials.GET("/categories", credentialHandler.GetCredentialCategories)
			credentials.POST("/impact", credentialHandler.BatchCredentialImpact)
			credentials.POST("/batch/delete", credentialHandler.BatchDeleteCredentials)
			credentials.GET("/:id", credentialHandler.GetCredential)
			credentials.GET("/:id/impact", credentialHandler.GetCredentialImpact)
			credentials.GET("/:id/payload", credentialHandler.GetCredentialPayload)
			credentials.PUT("/:id", credentialHandler.UpdateCredential)
			credentials.DELETE("/:id", credentialHandler.DeleteCredential)
			credentials.POST("/:id/verify", credentialHandler.VerifyCredential)
			credentials.GET("/:id/usage", credentialHandler.GetCredentialUsage)
		}

		resources := api.Group("/resources")
		resources.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			resourceHandler := handlers.NewResourceHandler()
			terminalHandler := handlers.NewTerminalSessionHandler()
			terminalHandler.WS = wsHandler
			resources.GET("", resourceHandler.ListResources)
			resources.POST("/verify", resourceHandler.VerifyResourceConnection)
			resources.POST("", resourceHandler.CreateResource)
			resources.GET("/:id", resourceHandler.GetResource)
			resources.GET("/:id/k8s/overview", resourceHandler.GetK8sOverview)
			resources.POST("/:id/k8s/namespaces/query", resourceHandler.QueryK8sNamespaces)
			resources.POST("/:id/k8s/resources/query", resourceHandler.QueryK8sResources)
			resources.POST("/:id/k8s/actions", resourceHandler.CreateK8sAction)
			resources.GET("/:id/actions", resourceHandler.ListResourceOperationAudits)
			resources.POST("/:id/terminal-sessions", terminalHandler.CreateResourceTerminalSession)
			resources.GET("/:id/terminal-sessions", terminalHandler.ListResourceTerminalSessions)
			resources.GET("/:id/terminal-sessions/:session_id", terminalHandler.GetResourceTerminalSession)
			resources.POST("/:id/terminal-sessions/:session_id/close", terminalHandler.CloseResourceTerminalSession)
			resources.POST("/:id/base-info/refresh", resourceHandler.RefreshResourceBaseInfo)
			resources.GET("/:id/credentials", resourceHandler.ListResourceCredentialBindings)
			resources.POST("/:id/credentials/bind", resourceHandler.BindResourceCredential)
			resources.DELETE("/:id/credentials/:binding_id", resourceHandler.UnbindResourceCredential)
			resources.PUT("/:id", resourceHandler.UpdateResource)
			resources.DELETE("/:id", resourceHandler.DeleteResource)
		}

		storeTemplates := api.Group("/store/templates")
		storeTemplates.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			storeTemplateHandler := handlers.NewStoreTemplateHandler()
			storeTemplates.GET("", storeTemplateHandler.ListTemplates)
			storeTemplates.POST("", storeTemplateHandler.CreateTemplate)
			storeTemplates.GET("/:id", storeTemplateHandler.GetTemplate)
			storeTemplates.PUT("/:id", storeTemplateHandler.UpdateTemplate)
			storeTemplates.DELETE("/:id", storeTemplateHandler.DeleteTemplate)
			storeTemplates.GET("/:id/versions", storeTemplateHandler.ListTemplateVersions)
			storeTemplates.POST("/:id/versions", storeTemplateHandler.CreateTemplateVersion)
			storeTemplates.PUT("/:id/versions/:version_id", storeTemplateHandler.UpdateTemplateVersion)
			storeTemplates.DELETE("/:id/versions/:version_id", storeTemplateHandler.DeleteTemplateVersion)
			storeTemplates.POST("/:id/chart/resolve", storeTemplateHandler.ResolveChartSource)
			storeTemplates.POST("/:id/versions/:version_id/preview", storeTemplateHandler.PreviewTemplateVersion)
			storeTemplates.POST("/:id/versions/:version_id/chart/upload", storeTemplateHandler.UploadTemplateVersionChart)
		}

		appStore := api.Group("/store/apps")
		appStore.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			storeTemplateHandler := handlers.NewStoreTemplateHandler()
			appStore.GET("", storeTemplateHandler.ListAppCatalog)
			appStore.POST("/:id/variants/:version_id/preview", storeTemplateHandler.PreviewAppVariant)
		}

		aiModels := api.Group("/store/ai-models")
		aiModels.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			aiModelHandler := handlers.NewAIModelCatalogHandler()
			aiModels.GET("", aiModelHandler.ListModels)
			aiModels.POST("/import", aiModelHandler.ImportModel)
		}

		aiProviders := api.Group("/store/ai-providers")
		aiProviders.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			aiProviderHandler := handlers.NewAIProviderHandler()
			aiProviders.GET("", aiProviderHandler.ListProviders)
			aiProviders.POST("", aiProviderHandler.CreateProvider)
			aiProviders.PUT("/:id", aiProviderHandler.UpdateProvider)
			aiProviders.DELETE("/:id", aiProviderHandler.DeleteProvider)
			aiProviders.GET("/:id/model-bindings", aiProviderHandler.ListBindings)
			aiProviders.POST("/:id/model-bindings", aiProviderHandler.CreateBinding)
			aiProviders.PUT("/:id/model-bindings/:binding_id", aiProviderHandler.UpdateBinding)
			aiProviders.DELETE("/:id/model-bindings/:binding_id", aiProviderHandler.DeleteBinding)
		}

		aiAgents := api.Group("/ai/agents")
		aiAgents.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			aiAgentHandler := handlers.NewAIAgentHandler()
			aiAgents.GET("", aiAgentHandler.ListAgents)
			aiAgents.POST("", aiAgentHandler.CreateAgent)
			aiAgents.PUT("/:id", aiAgentHandler.UpdateAgent)
			aiAgents.DELETE("/:id", aiAgentHandler.DeleteAgent)
			aiAgents.GET("/:id/runtime-profiles", aiAgentHandler.ListRuntimeProfiles)
			aiAgents.POST("/:id/runtime-profiles", aiAgentHandler.CreateRuntimeProfile)
			aiAgents.PUT("/:id/runtime-profiles/:profile_id", aiAgentHandler.UpdateRuntimeProfile)
			aiAgents.DELETE("/:id/runtime-profiles/:profile_id", aiAgentHandler.DeleteRuntimeProfile)
		}

		deployments := api.Group("/deployments")
		deployments.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			deploymentHandler := handlers.NewDeploymentHandler()
			deployments.GET("/requests", deploymentHandler.ListDeploymentRequests)
			deployments.POST("/requests", deploymentHandler.CreateDeploymentRequest)
			deployments.GET("/requests/:id", deploymentHandler.GetDeploymentRequest)
		}

		// Webhook管理
		webhooks := api.Group("/webhooks")
		webhooks.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			webhookHandler := handlers.NewWebhookHandler()
			webhooks.GET("", webhookHandler.ListConfigs)
			webhooks.POST("", webhookHandler.CreateConfig)
			webhooks.GET("/:id", webhookHandler.GetConfig)
			webhooks.PUT("/:id", webhookHandler.UpdateConfig)
			webhooks.DELETE("/:id", webhookHandler.DeleteConfig)
			webhooks.GET("/events", webhookHandler.ListEvents)
		}

		// 统计分析
		stats := api.Group("/stats")
		stats.Use(middleware.JWTAuth(), middleware.WorkspaceContext(), middleware.WorkspaceMemberRequired())
		{
			statsHandler := handlers.NewStatisticsHandler()
			stats.GET("/overview", statsHandler.GetOverview)
			stats.GET("/trend", statsHandler.GetTrend)
			stats.GET("/top-pipelines", statsHandler.GetTopPipelines)
		}
	}

	return router
}
