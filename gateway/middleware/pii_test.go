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

func TestPIIMiddleware_SWIFTBIC(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"请转账至SWIFT: DEUTDEDB，金额1000欧元"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_IBAN(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"对方账户 GB29NWBK60161331926819 请核实后转款"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_LoanAccount(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"贷款合同号: LN20260514-AB12345，请查询还款进度"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_SecuritiesAccount(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"我的证券账号: A123456789，帮我分析持仓"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_ChinaUnifiedCredit(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"企业统一社会信用代码: 91310000MA1FL3GJ5X"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_InsurancePolicy(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"保单号: PAIC2026051400123，请查询理赔状态"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 422, resp.StatusCode)
}

func TestPIIMiddleware_FinanceCleanPasses(t *testing.T) {
	app := setupPIIApp()
	body := `{"messages":[{"content":"请帮我分析这季度的财务报告趋势"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestScanForPII_SWIFTBIC(t *testing.T) {
	piiType, found := middleware.ScanForPII("wire to BIC: CHASUS33XXX for settlement")
	assert.True(t, found)
	assert.Equal(t, "swift_bic", piiType)
}

func TestScanForPII_IBAN(t *testing.T) {
	piiType, found := middleware.ScanForPII("please credit DE89370400440532013000 immediately")
	assert.True(t, found)
	assert.Equal(t, "iban", piiType)
}

func TestScanForPII_ChinaUnifiedCredit(t *testing.T) {
	piiType, found := middleware.ScanForPII("供应商信用代码 91310000MA1FL3GJ5X 请核实")
	assert.True(t, found)
	assert.Equal(t, "china_unified_credit", piiType)
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
