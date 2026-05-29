package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// PFIType identifies a class of Protected Financial Information.
type PFIType string

const (
	PFITypeIBAN       PFIType = "iban"           // e.g. GB29NWBK60161331926819
	PFITypeRoutingNum PFIType = "routing_number"  // ABA 9-digit, starts 00-36
	PFITypeAccountNum PFIType = "account_number"  // preceded by account/acct/a/c keyword
	PFITypeSWIFT      PFIType = "swift_bic"       // SWIFT/BIC 8-11 chars
	PFITypeCUSIP      PFIType = "cusip"           // 9-char alphanumeric security ID
	PFITypeISIN       PFIType = "isin"            // 2-letter country + 10-char alphanumeric
	PFITypeLEI        PFIType = "lei"             // 20-char alphanumeric entity ID
	PFITypeTaxID      PFIType = "tax_id_ein"      // EIN: XX-XXXXXXX
)

type pfiRule struct {
	pfiType PFIType
	re      *regexp.Regexp
}

// pfiPatterns lists financial identifier patterns that are NOT already
// blocked by piiPatterns (credit_card, en_us_ssn). IBAN and SWIFT here
// use standalone patterns with no keyword context requirement so that
// bare tokens in financial documents are also caught.
var pfiPatterns = []*pfiRule{
	// IBAN: 2 uppercase letters, 2 digits, 4–29 alphanumeric (total 15–34 chars).
	{PFITypeIBAN, regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{4}\d{7,25}\b`)},

	// ABA routing number: 9 digits, first two 00–36.
	{PFITypeRoutingNum, regexp.MustCompile(`\b0[0-3]\d{7}\b`)},

	// SWIFT/BIC: keyword-anchored to avoid matching common English words.
	// Matches: SWIFT/BIC/wire codes preceded by swift, bic, or wire keyword.
	{PFITypeSWIFT, regexp.MustCompile(`(?i)(?:swift|bic|wire)[\s:/]*[A-Z]{4}[A-Z]{2}[A-Z0-9]{2}(?:[A-Z0-9]{3})?`)},

	// CUSIP: 3 digits + 5 alphanumeric + 1 check digit.
	{PFITypeCUSIP, regexp.MustCompile(`\b[0-9]{3}[A-Z0-9]{5}[0-9]\b`)},

	// ISIN: 2 uppercase letters + 9 alphanumeric + 1 check digit.
	{PFITypeISIN, regexp.MustCompile(`\b[A-Z]{2}[A-Z0-9]{9}[0-9]\b`)},

	// LEI: 18 uppercase alphanumeric + 2 digits.
	{PFITypeLEI, regexp.MustCompile(`\b[A-Z0-9]{18}\d{2}\b`)},

	// EIN (Employer Identification Number): XX-XXXXXXX.
	// Require context word to avoid matching SSN-like patterns.
	{PFITypeTaxID, regexp.MustCompile(`(?i)(?:ein|employer\s+identification|tax\s+id(?:entification)?)[\s:#]*\d{2}-\d{7}`)},

	// Bank account number preceded by account/acct/a\/c keyword, 8–17 digits.
	{PFITypeAccountNum, regexp.MustCompile(`(?i)(?:account|acct|a/c)\s*[#:]?\s*\d{8,17}`)},
}

// ScanForPFI scans text against all PFI patterns and returns every matched
// PFI type. Returns nil when no financial identifiers are found.
func ScanForPFI(text string) []PFIType {
	// Uppercase once for case-insensitive patterns that rely on \b[A-Z].
	upper := strings.ToUpper(text)
	var found []PFIType
	seen := make(map[PFIType]struct{}, len(pfiPatterns))
	for _, rule := range pfiPatterns {
		input := upper
		// Keyword-anchored patterns (EIN, account) use (?i) so match against original.
		if rule.pfiType == PFITypeTaxID || rule.pfiType == PFITypeAccountNum {
			input = text
		}
		if rule.re.MatchString(input) {
			if _, ok := seen[rule.pfiType]; !ok {
				seen[rule.pfiType] = struct{}{}
				found = append(found, rule.pfiType)
			}
		}
	}
	return found
}

// NewPFIMiddleware returns a Fiber handler that runs PFI detection on the
// request body before the call is forwarded. When PFI is detected it sets:
//
//	c.Locals("pfi_detected", true)
//	c.Locals("pfi_types", []PFIType{...})
//
// A SIEM event is emitted non-blocking. The request is never blocked.
func NewPFIMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		types := ScanForPFI(string(c.Body()))
		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("pfi_detected", true)
		c.Locals("pfi_types", types)

		if siemChan != nil {
			tid, uid := "", ""
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
				EventType: "pfi_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "pfi_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":   uid,
						"pfi_types": typeStrs,
						"path":      c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return c.Next()
	}
}
