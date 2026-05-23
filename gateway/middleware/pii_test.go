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

// --- English PII tests ---

func TestScanForPII_EnUSSSN(t *testing.T) {
	piiType, found := middleware.ScanForPII("my SSN is 123-45-6789 please verify")
	assert.True(t, found)
	assert.Equal(t, "en_us_ssn", piiType)
}

func TestScanForPII_EnUSPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("call me at 555-123-4567 tomorrow")
	assert.True(t, found)
	assert.Equal(t, "en_us_phone", piiType)
}

func TestScanForPII_EnUKPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("reach me on +44 7700 900123")
	assert.True(t, found)
	assert.Equal(t, "en_uk_phone", piiType)
}

func TestScanForPII_EnUKNI(t *testing.T) {
	piiType, found := middleware.ScanForPII("NI number: AB 12 34 56 C")
	assert.True(t, found)
	assert.Equal(t, "en_uk_ni", piiType)
}

func TestScanForPII_EnSSNKeyword(t *testing.T) {
	piiType, found := middleware.ScanForPII("social security number: 234-56-7890")
	assert.True(t, found)
	assert.Contains(t, []string{"en_us_ssn", "en_ssn_keyword"}, piiType)
}

func TestScanForPII_EnDOB(t *testing.T) {
	piiType, found := middleware.ScanForPII("date of birth: 15/03/1990")
	assert.True(t, found)
	assert.Equal(t, "en_dob", piiType)
}

func TestScanForPII_EnMedicalRecord(t *testing.T) {
	piiType, found := middleware.ScanForPII("patient id: MRN-20260501A")
	assert.True(t, found)
	assert.Equal(t, "en_medical_record", piiType)
}

// --- Japanese PII tests ---

func TestScanForPII_JpMyNumber(t *testing.T) {
	piiType, found := middleware.ScanForPII("マイナンバー: 1234-5678-9012")
	assert.True(t, found)
	assert.Equal(t, "jp_my_number", piiType)
}

func TestScanForPII_JpPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("連絡先は090-1234-5678です")
	assert.True(t, found)
	assert.Equal(t, "jp_phone", piiType)
}

func TestScanForPII_JpPostal(t *testing.T) {
	piiType, found := middleware.ScanForPII("住所: 〒100-0001 東京都")
	assert.True(t, found)
	assert.Equal(t, "jp_postal", piiType)
}

func TestScanForPII_JpBankAccount(t *testing.T) {
	piiType, found := middleware.ScanForPII("口座番号: 1234567 に振り込んでください")
	assert.True(t, found)
	assert.Equal(t, "jp_bank_account", piiType)
}

// --- Korean PII tests ---

func TestScanForPII_KrRRN(t *testing.T) {
	piiType, found := middleware.ScanForPII("주민등록번호: 900101-1234567")
	assert.True(t, found)
	// keyword rule fires first
	assert.Contains(t, []string{"kr_rrn_keyword", "kr_rrn"}, piiType)
}

func TestScanForPII_KrPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("전화번호: 010-1234-5678")
	assert.True(t, found)
	assert.Equal(t, "kr_phone", piiType)
}

func TestScanForPII_KrBusinessReg(t *testing.T) {
	piiType, found := middleware.ScanForPII("사업자등록번호: 123-45-67890")
	assert.True(t, found)
	assert.Equal(t, "kr_business_reg", piiType)
}

// --- European PII tests ---

func TestScanForPII_FrPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("appelez le +33 6 12 34 56 78")
	assert.True(t, found)
	assert.Equal(t, "fr_phone", piiType)
}

func TestScanForPII_DePhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("Ruf mich an: +49 170 12345678")
	assert.True(t, found)
	assert.Equal(t, "de_phone", piiType)
}

func TestScanForPII_EsDNI(t *testing.T) {
	piiType, found := middleware.ScanForPII("mi DNI es 12345678Z")
	assert.True(t, found)
	assert.Equal(t, "es_dni_nie", piiType)
}

func TestScanForPII_EsPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("llámame al +34 612 345 678")
	assert.True(t, found)
	assert.Equal(t, "es_phone", piiType)
}

// --- More European countries ---

func TestScanForPII_ItCodiceFiscale(t *testing.T) {
	piiType, found := middleware.ScanForPII("codice fiscale: RSSMRA85T10A562S")
	assert.True(t, found)
	assert.Contains(t, []string{"it_codice_fiscale", "it_cf_keyword"}, piiType)
}

func TestScanForPII_ItPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("chiamami al +39 345 1234567")
	assert.True(t, found)
	assert.Equal(t, "it_phone", piiType)
}

func TestScanForPII_NlBSN(t *testing.T) {
	piiType, found := middleware.ScanForPII("mijn BSN: 123456782")
	assert.True(t, found)
	assert.Equal(t, "nl_bsn", piiType)
}

func TestScanForPII_NlPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("bel me op +31 6 12345678")
	assert.True(t, found)
	assert.Equal(t, "nl_phone", piiType)
}

func TestScanForPII_PlPesel(t *testing.T) {
	piiType, found := middleware.ScanForPII("PESEL: 85010112345")
	assert.True(t, found)
	assert.Equal(t, "pl_pesel", piiType)
}

func TestScanForPII_SePersonnummer(t *testing.T) {
	piiType, found := middleware.ScanForPII("personnummer: 850101-1234")
	assert.True(t, found)
	assert.Contains(t, []string{"se_personnummer", "se_personnummer_keyword"}, piiType)
}

func TestScanForPII_SePhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("ring mig på +46 70 123 45 67")
	assert.True(t, found)
	assert.Equal(t, "se_phone", piiType)
}

func TestScanForPII_PtNIF(t *testing.T) {
	piiType, found := middleware.ScanForPII("NIF: 123456789")
	assert.True(t, found)
	assert.Equal(t, "pt_nif", piiType)
}

func TestScanForPII_BeNationalNr(t *testing.T) {
	piiType, found := middleware.ScanForPII("numéro national: 85.01.01-123.45")
	assert.True(t, found)
	assert.Contains(t, []string{"be_national_nr", "be_national_keyword"}, piiType)
}

func TestScanForPII_ChAHV(t *testing.T) {
	piiType, found := middleware.ScanForPII("AHV-Nr: 756.1234.5678.90")
	assert.True(t, found)
	assert.Contains(t, []string{"ch_ahv", "ch_ahv_keyword"}, piiType)
}

func TestScanForPII_DkCPR(t *testing.T) {
	// DK CPR and SE personnummer share the same 6-digit-hyphen-4-digit format;
	// keyword rule disambiguates, bare-number rule may fire as either.
	piiType, found := middleware.ScanForPII("CPR-nummer: 010185-1234")
	assert.True(t, found)
	assert.Contains(t, []string{"dk_cpr", "dk_cpr_keyword", "se_personnummer", "se_personnummer_keyword"}, piiType)
}

func TestScanForPII_FiHETU(t *testing.T) {
	piiType, found := middleware.ScanForPII("henkilötunnus: 010185-123A")
	assert.True(t, found)
	assert.Contains(t, []string{"fi_hetu", "fi_hetu_keyword"}, piiType)
}

func TestScanForPII_NoFodselsnummer(t *testing.T) {
	// "01018512345" also matches ar_eg_phone (010 prefix); keyword ensures PII is caught.
	piiType, found := middleware.ScanForPII("fødselsnummer: 01018512345")
	assert.True(t, found)
	assert.Contains(t, []string{"no_fodselsnummer", "ar_eg_phone"}, piiType)
}

func TestScanForPII_NoPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("ring meg på +47 912 34 567")
	assert.True(t, found)
	assert.Equal(t, "no_phone", piiType)
}

// --- Arabic PII tests ---

func TestScanForPII_ArSaNationalID(t *testing.T) {
	piiType, found := middleware.ScanForPII("رقم الهوية: 1234567890")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_sa_national_id", "ar_sa_id_bare"}, piiType)
}

func TestScanForPII_ArSaIqama(t *testing.T) {
	piiType, found := middleware.ScanForPII("رقم الإقامة: 2123456789")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_sa_national_id", "ar_sa_id_bare"}, piiType)
}

func TestScanForPII_ArUaeEmiratesID(t *testing.T) {
	piiType, found := middleware.ScanForPII("Emirates ID: 784-1990-1234567-8")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_uae_emirates_id", "ar_uae_id_keyword"}, piiType)
}

func TestScanForPII_ArEgNationalID(t *testing.T) {
	// 14-digit EG ID also matches credit_card (13-16 digit rule fires first).
	piiType, found := middleware.ScanForPII("الرقم القومي: 29001011234567")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_eg_national_id", "ar_eg_id_bare", "credit_card"}, piiType)
}

func TestScanForPII_ArKwCivilID(t *testing.T) {
	piiType, found := middleware.ScanForPII("الرقم المدني: 123456789012")
	assert.True(t, found)
	assert.Equal(t, "ar_kw_civil_id", piiType)
}

func TestScanForPII_ArQaQID(t *testing.T) {
	piiType, found := middleware.ScanForPII("رقم القيد: 28123456789")
	assert.True(t, found)
	assert.Equal(t, "ar_qa_qid", piiType)
}

func TestScanForPII_ArPassport(t *testing.T) {
	// A + 8-digit format also matches en_us_passport; keyword ensures PII is caught.
	piiType, found := middleware.ScanForPII("رقم جواز السفر: A12345678")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_passport", "en_us_passport"}, piiType)
}

func TestScanForPII_ArBankAccount(t *testing.T) {
	// SA IBAN format — IBAN rule or ar_bank_account may fire
	_, found := middleware.ScanForPII("رقم الحساب البنكي: SA1234567890123456789012")
	assert.True(t, found)
}

func TestScanForPII_ArSaPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("اتصل بي على +966 51 2345 6789")
	assert.True(t, found)
	assert.Equal(t, "ar_sa_phone", piiType)
}

func TestScanForPII_ArUaePhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("call me at +971 50 123 4567")
	assert.True(t, found)
	assert.Equal(t, "ar_uae_phone", piiType)
}

func TestScanForPII_ArEgPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("رقم الهاتف: +20 10 1234 5678")
	assert.True(t, found)
	assert.Equal(t, "ar_eg_phone", piiType)
}

func TestScanForPII_ArQaPhone(t *testing.T) {
	piiType, found := middleware.ScanForPII("contact: +974 5123 4567")
	assert.True(t, found)
	assert.Equal(t, "ar_qa_phone", piiType)
}

func TestScanForPII_ArMaCIN(t *testing.T) {
	piiType, found := middleware.ScanForPII("رقم بطاقة التعريف الوطنية: AB123456")
	assert.True(t, found)
	assert.Equal(t, "ar_ma_cin", piiType)
}

func TestScanForPII_ArDzNIN(t *testing.T) {
	// 18-digit NIN may also trigger china_id_card (18-digit rule); both are valid PII detections.
	piiType, found := middleware.ScanForPII("رقم التعريف الوطني: 123456789012345678")
	assert.True(t, found)
	assert.Contains(t, []string{"ar_dz_nin", "china_id_card"}, piiType)
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
