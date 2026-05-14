package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/db"
	"github.com/yourorg/totra/admin/services"
)

func main() {
	cfg := config.Load()

	pool, err := db.Connect(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	auditSvc := services.NewAuditService(pool)

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)
	allowlistSvc := services.NewIPAllowlistService(pool)
	botSvc := services.NewBotService(pool, cfg.EncryptionKey)
	hrSyncSvc := services.NewHRSyncService(pool)
	agentSvc := services.NewAgentServiceFromPool(pool)
	retentionSvc := services.NewDataRetentionService(pool)
	deletionSvc := services.NewDeletionRequestService(pool)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	complianceSvc := services.NewComplianceService(pool)
	checklistSvc := services.NewChecklistService(pool)
	anomalySvc := services.NewAnomalyService(pool)
	complianceBenchmarkSvc := services.NewComplianceBenchmarkService(pool)
	internalSecret := os.Getenv("INTERNAL_SECRET")
	api.RegisterComplianceInternalRoutes(app, complianceSvc, botSvc, internalSecret)

	protected := app.Group("/", jwtMiddleware)
	protected.Use(services.IPAllowlistMiddleware(allowlistSvc))
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	usageSvc := services.NewUsageService(pool)
	api.RegisterUsageRoutes(protected, usageSvc)
	api.RegisterUsageAdminRoutes(protected, usageSvc)
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
	api.RegisterIPAllowlistRoutes(protected, allowlistSvc)
	api.RegisterBotRoutes(protected, botSvc)
	costSvc := services.NewCostAnalysisService(pool)
	api.RegisterCostAnalysisRoutes(protected, costSvc, botSvc)
	costSavingsSvc := services.NewCostSavingsReportService(pool)
	api.RegisterCostSavingsRoutes(protected, costSavingsSvc)
	budgetForecastSvc := services.NewBudgetForecastService(pool)
	costBenchmarkSvc := services.NewCostBenchmarkService(pool)
	api.RegisterCostAdvancedRoutes(protected, budgetForecastSvc, costBenchmarkSvc)
	api.RegisterHRSyncRoutes(protected, hrSyncSvc)
	api.RegisterAgentRoutes(protected, agentSvc)
	api.RegisterAuditRoutes(protected, auditSvc)
	api.RegisterGDPRRoutes(protected, retentionSvc, deletionSvc)
	api.RegisterComplianceRoutes(protected, complianceSvc)
	api.RegisterChecklistRoutes(protected, checklistSvc)
	api.RegisterComplianceAdvancedRoutes(protected, anomalySvc, complianceBenchmarkSvc)
	policyRulesSvc := services.NewPolicyRulesService(pool)
	api.RegisterPolicyRulesRoutes(protected, policyRulesSvc)

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
