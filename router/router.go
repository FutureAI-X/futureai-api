package router

import (
	"github.com/FutureAI-X/futureai-api/common"
	"github.com/FutureAI-X/futureai-api/controller"
	"github.com/FutureAI-X/futureai-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetRouter(server *gin.Engine) {
	// 健康检查接口
	server.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	})

	// 认证相关接口（无需登录）
	authRouter := server.Group("/api/auth")
	{
		authRouter.POST("/login", controller.Login)
	}

	// 公开接口（无需登录）
	pricingRouter := server.Group("/api/pricing")
	{
		pricingRouter.GET("", controller.GetPricing)
	}

	// 需要登录的接口
	userRouter := server.Group("/api/user")
	userRouter.Use(middleware.UserAuth())
	{
		userRouter.GET("/info", controller.GetUserInfo)
		userRouter.PUT("/profile", controller.UpdateProfile)
		userRouter.PUT("/password", controller.ResetMyPassword)

		userRouter.GET("/tokens", controller.GetTokens)
		userRouter.POST("/tokens", controller.CreateToken)
		userRouter.PUT("/tokens/:id", controller.UpdateToken)
		userRouter.DELETE("/tokens/:id", controller.DeleteToken)

		userRouter.GET("/credit-logs", controller.GetCreditLogs)

		userRouter.GET("/task-logs", controller.GetUserTaskLogs)
		userRouter.GET("/task-logs/:id", controller.GetUserTaskLogDetail)
	}

	// API 路由组（校验 API Key + 按用户限流）
	// 限流用于约束上游额度消耗：这些端点每次调用都会真实花费供应商配额。
	apiRouter := server.Group("/v1")
	apiRouter.Use(middleware.APIAuth())
	apiRouter.Use(middleware.RateLimit(
		common.GetEnvOrDefault("API_RATE_LIMIT_PER_MINUTE", 60),
		common.GetEnvOrDefault("API_RATE_LIMIT_BURST", 10),
	))
	{
		// 模型列表接口
		apiRouter.GET("/models", controller.ListModels)

		// 图像生成
		apiRouter.POST("/images/generations", controller.ImageGenerate)

		// 图片上传
		apiRouter.POST("/uploads/images", controller.UploadImage)

		// 任务查询
		apiRouter.GET("/tasks/:task_id", controller.GetTask)
	}

	// 管理员接口
	adminRouter := server.Group("/api/admin")
	adminRouter.Use(middleware.RootAuth())
	{
		adminRouter.GET("/users", controller.AdminGetUsers)
		adminRouter.POST("/users", controller.AdminCreateUser)
		adminRouter.PUT("/users/:id", controller.AdminUpdateUser)
		adminRouter.DELETE("/users/:id", controller.AdminDeleteUser)
		adminRouter.PUT("/users/:id/status", controller.AdminUpdateUserStatus)
		adminRouter.PUT("/users/:id/credits", controller.AdminAdjustUserCredits)
		adminRouter.PUT("/users/:id/password", controller.AdminResetPassword)

		adminRouter.GET("/vendors", controller.AdminGetVendors)
		adminRouter.POST("/vendors", controller.AdminCreateVendor)
		adminRouter.PUT("/vendors/:id", controller.AdminUpdateVendor)
		adminRouter.PUT("/vendors/:id/status", controller.AdminUpdateVendorStatus)
		adminRouter.DELETE("/vendors/:id", controller.AdminDeleteVendor)

		adminRouter.GET("/vendor-models", controller.AdminGetVendorModels)
		adminRouter.POST("/vendor-models", controller.AdminCreateVendorModel)
		adminRouter.PUT("/vendor-models/:id", controller.AdminUpdateVendorModel)
		adminRouter.PUT("/vendor-models/:id/status", controller.AdminUpdateVendorModelStatus)
		adminRouter.DELETE("/vendor-models/:id", controller.AdminDeleteVendorModel)

		adminRouter.GET("/endpoints", controller.AdminGetEndpoints)
		adminRouter.POST("/endpoints", controller.AdminCreateEndpoint)
		adminRouter.PUT("/endpoints/:id", controller.AdminUpdateEndpoint)
		adminRouter.PUT("/endpoints/:id/status", controller.AdminUpdateEndpointStatus)
		adminRouter.DELETE("/endpoints/:id", controller.AdminDeleteEndpoint)

		adminRouter.GET("/models", controller.AdminGetModels)
		adminRouter.POST("/models", controller.AdminCreateModel)
		adminRouter.PUT("/models/:id", controller.AdminUpdateModel)
		adminRouter.PUT("/models/:id/status", controller.AdminUpdateModelStatus)
		adminRouter.DELETE("/models/:id", controller.AdminDeleteModel)

		adminRouter.GET("/models/:id/endpoints", controller.GetModelEndpoints)
		adminRouter.PUT("/models/:id/endpoints", controller.SyncModelEndpoints)

		adminRouter.GET("/models/:id/credit-rule", controller.AdminGetCreditRule)
		adminRouter.POST("/models/:id/credit-rule", controller.AdminSaveCreditRule)
		adminRouter.DELETE("/models/:id/credit-rule", controller.AdminDeleteModelCreditRule)

		adminRouter.PUT("/credit-rules/:id/status", controller.AdminUpdateCreditRuleStatus)
		adminRouter.DELETE("/credit-rules/:id", controller.AdminDeleteCreditRule)

		adminRouter.GET("/credit-logs", controller.AdminGetCreditLogs)

		adminRouter.GET("/task-logs", controller.AdminGetTaskLogs)
		adminRouter.GET("/task-logs/:id", controller.AdminGetTaskLogDetail)
	}
}
