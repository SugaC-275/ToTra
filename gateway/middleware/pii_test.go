package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)

func setupPIIApp() *fiber.App {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "test-tenant", nil))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		return c.SendStatus(200)
	})
	return app
}

func TestPIIMiddleware_CleanRequest(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"How do I sort a list in Go?"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPIIMiddleware_PhoneNumber(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Call customer at 13800001234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_ChinaIDCard(t *testing.T) {
	app := setupPIIApp()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"ID: 110101199001011234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_EmailBlocked(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "tenant-1", nil))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	body := `{"messages":[{"content":"联系我 foo@example.com 获取报价"}]}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_CleanRequestPasses(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "tenant-1", nil))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendString("ok") })
	body := `{"messages":[{"content":"帮我写一个排序算法"}]}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestPIIMiddleware_ContractAmount(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"审核合同金额：12,345,678元的采购合同"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_TransactionID(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"查询交易流水号：TX20260514001234的状态"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_PatientID(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"患者ID: P20260514001的检查报告"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_ICDCode(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"患者诊断：ICD-10-A01.0，请给出治疗方案"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestScanForPII_Phone(t *testing.T) {
	piiType, found := middleware.ScanForPII("please call 13812345678 for details")
	assert.True(t, found)
	assert.Equal(t, "china_phone", piiType)
}

func TestScanForPII_Clean(t *testing.T) {
	_, found := middleware.ScanForPII("this text contains no sensitive information")
	assert.False(t, found)
}

func TestScanForPII_IDCard(t *testing.T) {
	piiType, found := middleware.ScanForPII("身份证: 110101199001011234")
	assert.True(t, found)
	assert.Equal(t, "china_id_card", piiType)
}

func TestPIIMiddleware_SIEMChannelFired(t *testing.T) {
	ch := make(chan middleware.SIEMEvent, 1)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(middleware.NewPIIMiddleware(nil, "", ch))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"messages":[{"content":"call 13800001234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)

	require.Len(t, ch, 1)
	ev := <-ch
	assert.Equal(t, "t1", ev.TenantID)
	assert.Equal(t, "pii_violation", ev.EventType)
}

func TestPIIMiddleware_SIEMChannelNilSafe(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewPIIMiddleware(nil, "t1", nil))
	app.Post("/", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	body := `{"messages":[{"content":"call 13800001234"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode) // must not panic with nil channel
}
