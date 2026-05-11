package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type stubROISvc struct{}

func (s *stubROISvc) GetQuarterlyROI(_ context.Context, _, _ string) ([]*services.QuarterROI, error) {
	return []*services.QuarterROI{{Quarter: "2026-Q2", TotalUSD: 10, TotalOutput: 5, ROIScore: 0.5, ActiveUsers: 3}}, nil
}
func (s *stubROISvc) GetBenchmark(_ context.Context, _, _ string) (*services.BenchmarkResult, error) {
	return &services.BenchmarkResult{TenantAvgEfficiency: 0.20, Percentile: 50, Label: "Top 50%"}, nil
}
func (s *stubROISvc) GetDeptChallenge(_ context.Context, _, _ string) ([]*services.DeptChallengeEntry, error) {
	return []*services.DeptChallengeEntry{{Department: "Engineering", AvgAIQScore: 80, UserCount: 2, Rank: 1, IsWinner: true}}, nil
}

func setupROIApp(svc *stubROISvc) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "admin"})
		return c.Next()
	})
	api.RegisterROIRoutes(app, svc)
	return app
}

func TestROIQuarterly_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/quarterly?year=2026", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Len(t, result, 1)
	assert.Equal(t, "2026-Q2", result[0]["quarter"])
}

func TestROIBenchmark_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/benchmark?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "Top 50%", result["label"])
}

func TestROIChallenge_OK(t *testing.T) {
	app := setupROIApp(&stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/challenge?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	var result []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Len(t, result, 1)
	assert.Equal(t, true, result[0]["is_winner"])
}

func TestROI_NonAdmin_Forbidden(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid", TenantID: "tid", Role: "employee"})
		return c.Next()
	})
	api.RegisterROIRoutes(app, &stubROISvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roi/quarterly?year=2026", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 403, resp.StatusCode)
}
