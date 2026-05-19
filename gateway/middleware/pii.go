package middleware

import (
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
)

type piiRule struct {
	name string
	re   *regexp.Regexp
}

var piiPatterns = []*piiRule{
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "china_phone", re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
	{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{name: "bank_account", re: regexp.MustCompile(`\b\d{16,19}\b`)},
	// Finance industry
	{name: "contract_amount", re: regexp.MustCompile(`(?:合同金额|合同价款)[：:\s]*[¥￥]?\d[\d,\.]*(?:万|千万|亿)?元?`)},
	{name: "transaction_id", re: regexp.MustCompile(`(?:交易流水号|流水号|交易编号)[：:\s]*[A-Za-z0-9\-]{8,}`)},
	{name: "swift_bic", re: regexp.MustCompile(`(?i)(?:swift|bic)[\s:：码代号]*[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}(?:[A-Z0-9]{3})?`)},
	{name: "iban", re: regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{11,29}\b`)},
	{name: "loan_account", re: regexp.MustCompile(`(?:贷款合同[号编]|借款合同编号|贷款账[户号])[：:\s]*[A-Za-z0-9\-]{6,30}`)},
	{name: "securities_account", re: regexp.MustCompile(`(?:证券账[户号]|股票账[户号]|基金账[户号])[：:\s]*[A-Za-z0-9]{6,12}`)},
	{name: "china_unified_credit", re: regexp.MustCompile(`\b[0-9A-HJ-NP-RT-Y]{2}\d{6}[0-9A-HJ-NP-RT-Y]{10}\b`)},
	{name: "insurance_policy", re: regexp.MustCompile(`(?:保险单号|保单号|保单编号)[：:\s]*[A-Za-z0-9\-]{8,25}`)},
	// Medical industry
	{name: "patient_id", re: regexp.MustCompile(`(?:患者[Ii][Dd]|患者编号|病历号|住院号)[：:\s]*[A-Za-z0-9\-]{4,20}`)},
	{name: "icd_code", re: regexp.MustCompile(`\bICD[-–]?(?:10|11)[-–]\s*[A-Z]\d{2}(?:\.\d{1,4})?\b`)},
}

// ViolationRecorder is the interface PIIMiddleware uses to record violations.
// This avoids an import cycle with the storage package.
type ViolationRecorder interface {
	RecordViolation(tenantID, userID, piiType, action, requestPath string)
}

// NewPIIMiddleware creates a PII detection middleware.
// recorder may be nil (no persistence). tenantID is used as a fallback when
// no authenticated user is present in the context; pass "" to always pull from
// the "user" local set by NewAuthMiddleware.
// siemChan is an optional channel for non-blocking SIEM event delivery; pass
// nil to disable SIEM integration.
func NewPIIMiddleware(recorder ViolationRecorder, tenantID string, siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
				tid := tenantID
				uid := ""
				if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
					tid = user.TenantID
					uid = user.UserID
				}
				if recorder != nil {
					recorder.RecordViolation(tid, uid, rule.name, "blocked", c.Path())
				}
				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  tid,
						EventType: "pii_violation",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   tid,
							"event_type":  "pii_violation",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":  uid,
								"pii_type": rule.name,
								"action":   "blocked",
								"path":     c.Path(),
							},
						},
					}:
					default: // drop if full
					}
				}
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential PII detected (" + rule.name + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}

// ScanForPII scans text against all PII patterns. Returns the matched PII type
// name and true on the first match; returns ("", false) if no PII is found.
func ScanForPII(text string) (piiType string, found bool) {
	for _, rule := range piiPatterns {
		if rule.re.MatchString(text) {
			return rule.name, true
		}
	}
	return "", false
}
