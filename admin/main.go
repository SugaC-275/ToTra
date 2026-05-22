package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/db"
	adminhandlers "github.com/yourorg/totra/admin/handlers"
	adminmiddleware "github.com/yourorg/totra/admin/middleware"
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

	// New services
	userSvc := services.NewUserService(pool)
	oidcSvc := services.NewOIDCService(pool, cfg.EncryptionKey)
	rbacSvc := services.NewRBACService(pool)
	alertPushSvc := services.NewAlertPushService(pool)
	empSelfSvc := services.NewEmployeeSelfService(pool)
	retentionMeta := &pgCleanupMeta{pool: pool}

	adminMetrics := adminmiddleware.NewAdminMetrics(prometheus.DefaultRegisterer)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))
	app.Use(adminmiddleware.NewAdminMetricsMiddleware(adminMetrics))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/metrics", adminhandlers.MetricsHandler(prometheus.DefaultGatherer))

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))
	// Public OIDC routes (no JWT required — login redirect and callback)
	api.RegisterOIDCPublicRoutes(app, oidcSvc, userSvc, jwtSvc)

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

	api.RegisterUserRoutes(protected, userSvc)
	api.RegisterModelRoutes(protected, services.NewModelService(pool, cfg.EncryptionKey))
	usageSvc := services.NewUsageService(pool)
	api.RegisterUsageRoutes(protected, usageSvc)
	api.RegisterUsageAdminRoutes(protected, usageSvc)
	quotaSvc := services.NewQuotaService(pool)
	api.RegisterQuotaRoutes(protected, quotaSvc)
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
	costSettingsSvc := services.NewCostSettingsService(pool)
	budgetStatusSvc := services.NewBudgetStatusService(pool)
	offHoursSvc := services.NewOffHoursService(pool)
	api.RegisterCostSettingsRoutes(protected, costSettingsSvc, budgetStatusSvc, offHoursSvc)
	topSpendersSvc := services.NewTopSpendersService(pool)
	budgetAlertSvc := services.NewBudgetAlertService(pool)
	budgetAlertSvc.SetPushService(alertPushSvc)
	api.RegisterCostReportsRoutes(protected, topSpendersSvc, budgetAlertSvc, botSvc)
	optSuggestionsSvc := services.NewOptimizationSuggestionService(pool)
	api.RegisterCostOptimizationRoutes(protected, optSuggestionsSvc)
	procurementSvc := services.NewProcurementReportService(pool)
	api.RegisterProcurementRoutes(protected, procurementSvc)
	budgetPlannerSvc := services.NewBudgetPlannerService(pool)
	api.RegisterBudgetPlannerRoutes(protected, budgetPlannerSvc)
	api.RegisterHRSyncRoutes(protected, hrSyncSvc)
	api.RegisterAgentRoutes(protected, agentSvc)
	api.RegisterAuditRoutes(protected, auditSvc)
	api.RegisterGDPRRoutes(protected, retentionSvc, deletionSvc)
	api.RegisterComplianceRoutes(protected, complianceSvc)
	api.RegisterChecklistRoutes(protected, checklistSvc)
	api.RegisterComplianceAdvancedRoutes(protected, anomalySvc, complianceBenchmarkSvc)
	riskTrendSvc := services.NewRiskTrendService(pool)
	complianceDigestSvc := services.NewComplianceDigestService(pool)
	api.RegisterComplianceReportRoutes(protected, riskTrendSvc, complianceDigestSvc)
	policyRulesSvc := services.NewPolicyRulesService(pool)
	api.RegisterPolicyRulesRoutes(protected, policyRulesSvc)

	siemCfgSvc := services.NewSIEMConfigService(pool, cfg.EncryptionKey)
	siemDelivSvc := services.NewSIEMDeliveryService(pool, cfg.EncryptionKey)
	siemPullSvc := services.NewSIEMPullService(pool)
	go siemDelivSvc.RunWorker(context.Background())
	api.RegisterSIEMRoutes(protected, siemCfgSvc, siemDelivSvc, siemPullSvc)

	// New protected routes
	api.RegisterOIDCAdminRoutes(protected, oidcSvc)
	api.RegisterRBACRoutes(protected, rbacSvc)
	api.RegisterAlertConfigRoutes(protected, alertPushSvc)
	api.RegisterEmployeeSelfServiceRoutes(protected, empSelfSvc, quotaSvc)
	api.RegisterDataRetentionRoutes(protected, retentionSvc, retentionMeta)

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}

// pgCleanupMeta implements api.RetentionCleanupMetaStore backed by tenant_cost_settings.
type pgCleanupMeta struct{ pool *pgxpool.Pool }

func (m *pgCleanupMeta) GetCleanupMeta(ctx context.Context, tenantID string) (*api.RetentionCleanupMeta, error) {
	var meta api.RetentionCleanupMeta
	err := m.pool.QueryRow(ctx,
		`SELECT last_cleanup_at, last_cleanup_rows_deleted FROM tenant_cost_settings WHERE tenant_id = $1`,
		tenantID,
	).Scan(&meta.LastCleanupAt, &meta.LastCleanupRowsDeleted)
	if err == pgx.ErrNoRows {
		return &meta, nil
	}
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (m *pgCleanupMeta) SaveCleanupMeta(ctx context.Context, tenantID string, rowsDeleted int64, at time.Time) error {
	_, err := m.pool.Exec(ctx,
		`INSERT INTO tenant_cost_settings (tenant_id, last_cleanup_at, last_cleanup_rows_deleted)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (tenant_id) DO UPDATE
		 SET last_cleanup_at = EXCLUDED.last_cleanup_at,
		     last_cleanup_rows_deleted = EXCLUDED.last_cleanup_rows_deleted`,
		tenantID, at, rowsDeleted,
	)
	return err
}
