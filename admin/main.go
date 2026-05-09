package main

import (
	"log"

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

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	protected := app.Group("/", jwtMiddleware)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
