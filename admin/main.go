package main

import (
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/cron"
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

	kpiSvc := services.NewKPIService(pool)
	fuelSvc := services.NewFuelService(pool)
	cron.StartMonthlySnapshot(pool, kpiSvc, fuelSvc)

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)
	allowlistSvc := services.NewIPAllowlistService(pool)
	botSvc := services.NewBotService(pool, cfg.EncryptionKey)
	insightsSvc := services.NewAIInsightsService(pool, os.Getenv("ANTHROPIC_API_KEY"))

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

	protected := app.Group("/", jwtMiddleware)
	protected.Use(services.IPAllowlistMiddleware(allowlistSvc))
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	usageSvc := services.NewUsageService(pool)
	api.RegisterUsageRoutes(protected, usageSvc)
	api.RegisterUsageAdminRoutes(protected, usageSvc)
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
	api.RegisterKPIRoutes(protected, kpiSvc)
	api.RegisterFuelRoutes(protected, fuelSvc)
	api.RegisterIPAllowlistRoutes(protected, allowlistSvc)
	api.RegisterBotRoutes(protected, botSvc)
	api.RegisterAIInsightsRoutes(protected, insightsSvc)

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
