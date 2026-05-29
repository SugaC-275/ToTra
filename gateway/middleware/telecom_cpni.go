package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CPNIType identifies a category of Customer Proprietary Network Information
// regulated by the FCC under 47 U.S.C. §222.
type CPNIType string

const (
	CPNICallRecords  CPNIType = "call_records"   // CDR, call detail records
	CPNINetworkUsage CPNIType = "network_usage"  // data usage, bandwidth
	CPNIBillingData  CPNIType = "billing_data"   // billing records, account charges
	CPNIServiceInfo  CPNIType = "service_info"   // subscriptions, plan details, IMSI, IMEI
	CPNILocationData CPNIType = "location_data"  // cell tower, network location
)

type cpniRule struct {
	cpniType CPNIType
	re       *regexp.Regexp
}

var cpniPatterns = []*cpniRule{
	// --- Call records / CDR ---
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)call\s+detail\s+record`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`\bCDR\b`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)call\s+log`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)call\s+history`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)dialed\s+number`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)incoming\s+call.{0,40}(?:record|log|history|data)`),
	},
	{
		cpniType: CPNICallRecords,
		re:       regexp.MustCompile(`(?i)call\s+duration.{0,60}(?:\d+\s*(?:second|minute|hour|sec|min|hr))`),
	},

	// --- Network usage ---
	{
		cpniType: CPNINetworkUsage,
		re:       regexp.MustCompile(`(?i)data\s+usage.{0,40}(?:[0-9]*\s*(?:GB|MB|TB|gigabyte|megabyte)|this\s+month|per\s+(?:month|day|cycle))`),
	},
	{
		cpniType: CPNINetworkUsage,
		re:       regexp.MustCompile(`(?i)bandwidth\s+consumption`),
	},
	{
		cpniType: CPNINetworkUsage,
		re:       regexp.MustCompile(`(?i)network\s+traffic.{0,60}(?:subscriber|account|user|customer)`),
	},
	{
		cpniType: CPNINetworkUsage,
		re:       regexp.MustCompile(`(?i)packet\s+data.{0,60}(?:account|subscriber|customer)`),
	},

	// --- Billing data ---
	{
		cpniType: CPNIBillingData,
		re:       regexp.MustCompile(`(?i)billing\s+record`),
	},
	{
		cpniType: CPNIBillingData,
		re:       regexp.MustCompile(`(?i)invoice.{0,60}(?:wireless|telecom|broadband|carrier|cellular|mobile|MVNO|phone)`),
	},
	{
		cpniType: CPNIBillingData,
		re:       regexp.MustCompile(`(?i)account\s+charge`),
	},
	{
		cpniType: CPNIBillingData,
		re:       regexp.MustCompile(`(?i)roaming\s+charge`),
	},

	// --- Service / Subscription info ---
	{
		cpniType: CPNIServiceInfo,
		re:       regexp.MustCompile(`(?i)service\s+plan`),
	},
	{
		cpniType: CPNIServiceInfo,
		re:       regexp.MustCompile(`(?i)subscription.{0,60}(?:wireless|broadband|MVNO|carrier|cellular|mobile)`),
	},
	// IMSI: exactly 15 digits (keyword or bare number)
	{
		cpniType: CPNIServiceInfo,
		re:       regexp.MustCompile(`(?i)IMSI[\s:]*\d{15}`),
	},
	{
		cpniType: CPNIServiceInfo,
		re:       regexp.MustCompile(`\b\d{15}\b`),
	},
	// IMEI: 15-17 digits (keyword required to reduce false positives)
	{
		cpniType: CPNIServiceInfo,
		re:       regexp.MustCompile(`(?i)IMEI[\s:]*\d{15,17}`),
	},

	// --- Location data ---
	{
		cpniType: CPNILocationData,
		re:       regexp.MustCompile(`(?i)cell\s+tower`),
	},
	{
		cpniType: CPNILocationData,
		re:       regexp.MustCompile(`(?i)network\s+location`),
	},
	{
		cpniType: CPNILocationData,
		re:       regexp.MustCompile(`(?i)tower\s+(?:ID|identifier)`),
	},
	{
		cpniType: CPNILocationData,
		re:       regexp.MustCompile(`(?i)\beNodeB\b`),
	},
	// MSISDN: keyword required
	{
		cpniType: CPNILocationData,
		re:       regexp.MustCompile(`(?i)MSISDN`),
	},
}

// ScanForCPNI returns the distinct CPNI data types detected in text.
// Returns an empty slice when no CPNI-protected content is found.
func ScanForCPNI(text string) []CPNIType {
	seen := make(map[CPNIType]struct{})
	_ = strings.ToLower(text) // patterns use (?i); kept for future keyword expansion

	for _, rule := range cpniPatterns {
		if rule.re.MatchString(text) {
			seen[rule.cpniType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []CPNIType{}
	}

	result := make([]CPNIType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewTelecomCPNIMiddleware returns a Fiber middleware that detects CPNI data
// in request bodies (FCC 47 U.S.C. §222). On detection it:
//   - Sets c.Locals("cpni_detected", true)
//   - Sets c.Locals("cpni_types", []CPNIType{...})
//   - Adds the X-CPNI-Detected: true response header
//   - Sends a non-blocking SIEMEvent with EventType "cpni_detected"
//
// The middleware does NOT block the request; enforcement is left to upstream policy.
func NewTelecomCPNIMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForCPNI(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("cpni_detected", true)
		c.Locals("cpni_types", types)
		c.Set("X-CPNI-Detected", "true")

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			typeStrs := make([]string, len(types))
			for i, t := range types {
				typeStrs[i] = string(t)
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "cpni_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "cpni_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":     uid,
						"cpni_types":  typeStrs,
						"path":        c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
