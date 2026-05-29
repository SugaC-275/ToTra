package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
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
	gatewaystorage "github.com/yourorg/totra/gateway/storage"
)

// loginRateLimiter enforces max 10 attempts per IP per 15 minutes.
type loginRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rateLimitEntry
}

type rateLimitEntry struct {
	count     int
	windowEnd time.Time
}

const (
	loginRateWindow   = 15 * time.Minute
	loginRateMaxHits  = 10
)

func newLoginRateLimiter() *loginRateLimiter {
	rl := &loginRateLimiter{entries: make(map[string]*rateLimitEntry)}
	go rl.evict()
	return rl
}

// evict removes stale entries every 15 minutes to prevent unbounded growth.
func (rl *loginRateLimiter) evict() {
	ticker := time.NewTicker(loginRateWindow)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		rl.mu.Lock()
		for ip, e := range rl.entries {
			if now.After(e.windowEnd) {
				delete(rl.entries, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow returns (allowed bool, retryAfter duration).
func (rl *loginRateLimiter) Allow(ip string) (bool, time.Duration) {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e, ok := rl.entries[ip]
	if !ok || now.After(e.windowEnd) {
		rl.entries[ip] = &rateLimitEntry{count: 1, windowEnd: now.Add(loginRateWindow)}
		return true, 0
	}
	e.count++
	if e.count > loginRateMaxHits {
		return false, time.Until(e.windowEnd)
	}
	return true, 0
}

// clientIP extracts the real IP from X-Forwarded-For or the remote address.
func clientIP(c *fiber.Ctx) string {
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		if host, _, err := net.SplitHostPort(xff); err == nil {
			return host
		}
		return xff
	}
	host, _, err := net.SplitHostPort(c.IP())
	if err != nil {
		return c.IP()
	}
	return host
}

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
	samlSvc := services.NewSAMLService(pool)
	rbacSvc := services.NewRBACService(pool)
	alertPushSvc := services.NewAlertPushService(pool)
	empSelfSvc := services.NewEmployeeSelfService(pool)
	retentionMeta := &pgCleanupMeta{pool: pool}

	adminMetrics := adminmiddleware.NewAdminMetrics(prometheus.DefaultRegisterer)

	// B4: HTTP timeouts
	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	})

	// W3: CORS origin from env, safe default for dev
	corsOrigin := os.Getenv("CORS_ALLOW_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "http://localhost:3000"
	}
	app.Use(cors.New(cors.Config{AllowOrigins: corsOrigin}))
	app.Use(adminmiddleware.NewAdminMetricsMiddleware(adminMetrics))

	// W7: health checks DB connectivity
	app.Get("/health", func(c *fiber.Ctx) error {
		if err := pool.Ping(context.Background()); err != nil {
			return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/metrics", adminhandlers.MetricsHandler(prometheus.DefaultGatherer))

	// W5: rate-limit login — max 10 attempts per IP per 15 minutes
	loginRL := newLoginRateLimiter()
	app.Post("/api/auth/login", func(c *fiber.Ctx) error {
		ip := clientIP(c)
		ok, retryAfter := loginRL.Allow(ip)
		if !ok {
			c.Set("Retry-After", strconv.FormatInt(int64(retryAfter.Seconds()), 10))
			return c.Status(http.StatusTooManyRequests).JSON(fiber.Map{
				"error": "too many login attempts, please try again later",
			})
		}
		return api.LoginHandler(pool, jwtSvc)(c)
	})

	// Public OIDC routes (no JWT required — login redirect and callback)
	api.RegisterOIDCPublicRoutes(app, oidcSvc, userSvc, jwtSvc)

	// Public SAML SP routes (no JWT required — metadata, login redirect, ACS)
	api.RegisterSAMLPublicRoutes(app, samlSvc, userSvc, jwtSvc)

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	complianceSvc := services.NewComplianceService(pool)
	checklistSvc := services.NewChecklistService(pool)
	anomalySvc := services.NewAnomalyService(pool)
	complianceBenchmarkSvc := services.NewComplianceBenchmarkService(pool)
	// B2: InternalSecret is now required at startup (validated in config.Load via mustGetEnv)
	api.RegisterComplianceInternalRoutes(app, complianceSvc, botSvc, cfg.InternalSecret)

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
	baaStore := services.NewBAAStore(pool)
	api.RegisterBAARoutes(protected, baaStore)
	complianceBundleSvc := services.NewComplianceBundleService(pool)
	api.RegisterComplianceBundleRoutes(protected, complianceBundleSvc)

	siemCfgSvc := services.NewSIEMConfigService(pool, cfg.EncryptionKey)
	siemDelivSvc := services.NewSIEMDeliveryService(pool, cfg.EncryptionKey)
	siemPullSvc := services.NewSIEMPullService(pool)
	go siemDelivSvc.RunWorker(context.Background())
	api.RegisterSIEMRoutes(protected, siemCfgSvc, siemDelivSvc, siemPullSvc)

	// New protected routes
	api.RegisterOIDCAdminRoutes(protected, oidcSvc)
	api.RegisterSAMLAdminRoutes(protected, samlSvc)
	api.RegisterRBACRoutes(protected, rbacSvc)
	api.RegisterAlertConfigRoutes(protected, alertPushSvc)
	api.RegisterEmployeeSelfServiceRoutes(protected, empSelfSvc, quotaSvc)
	api.RegisterDataRetentionRoutes(protected, retentionSvc, retentionMeta)
	api.RegisterMCPServerRoutes(protected, services.NewMCPServerService(pool, cfg.EncryptionKey))

	// Feature: model pricing auto-sync
	pricingSyncSvc := services.NewPricingSyncService(pool, alertPushSvc)
	api.RegisterPricingRoutes(protected, pricingSyncSvc)

	// Feature: DB-driven guardrail config
	adminhandlers.RegisterGuardrailRoutes(protected, &guardrailStoreAdapter{
		inner: gatewaystorage.NewGuardrailStore(pool, nil),
	})

	// W8: graceful shutdown on SIGTERM/SIGINT
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	go func() {
		log.Printf("Admin service listening on :%s", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Printf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down admin service...")
	if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
		log.Printf("shutdown: %v", err)
	}
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

// guardrailStoreAdapter converts gateway/storage.GuardrailStore to the
// adminhandlers.GuardrailStore interface using services.GuardrailConfig.
type guardrailStoreAdapter struct {
	inner *gatewaystorage.GuardrailStore
}

func (a *guardrailStoreAdapter) ListGuardrailConfigs(ctx context.Context, tenantID string) ([]*services.GuardrailConfig, error) {
	rows, err := a.inner.ListGuardrailConfigs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	result := make([]*services.GuardrailConfig, 0, len(rows))
	for _, r := range rows {
		result = append(result, &services.GuardrailConfig{
			ID:           r.ID,
			TenantID:     r.TenantID,
			Name:         r.Name,
			Enabled:      r.Enabled,
			Strictness:   r.Strictness,
			CustomConfig: r.CustomConfig,
			BundleID:     r.BundleID,
		})
	}
	return result, nil
}

func (a *guardrailStoreAdapter) UpsertGuardrailConfig(ctx context.Context, tenantID, name string, enabled bool, strictness string, customConfig map[string]any, bundleID *string) (*services.GuardrailConfig, error) {
	r, err := a.inner.UpsertGuardrailConfig(ctx, tenantID, name, enabled, strictness, customConfig, bundleID)
	if err != nil {
		return nil, err
	}
	return &services.GuardrailConfig{
		ID:           r.ID,
		TenantID:     r.TenantID,
		Name:         r.Name,
		Enabled:      r.Enabled,
		Strictness:   r.Strictness,
		CustomConfig: r.CustomConfig,
		BundleID:     r.BundleID,
	}, nil
}

func (a *guardrailStoreAdapter) DeleteGuardrailConfig(ctx context.Context, tenantID, name string) error {
	return a.inner.DeleteGuardrailConfig(ctx, tenantID, name)
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
