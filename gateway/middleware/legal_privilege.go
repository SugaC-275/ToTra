package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// PrivilegeType identifies a class of legal privilege signal.
type PrivilegeType string

const (
	PrivilegeTypeAttorneyClient PrivilegeType = "attorney_client"
	PrivilegeTypeWorkProduct    PrivilegeType = "work_product"
	PrivilegeTypeLitigationHold PrivilegeType = "litigation_hold"
)

type privilegeRule struct {
	privilegeType PrivilegeType
	re            *regexp.Regexp
}

// privilegePatterns matches communication metadata signals — labels, headers,
// and contextual phrases — that indicate attorney-client privilege or related
// legal doctrines. These are not content patterns; they target the surrounding
// framing that communicators use to assert privilege.
var privilegePatterns = []*privilegeRule{
	// --- Attorney-Client markers ---

	// Explicit privilege labels (header/footer stamps)
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\bprivileged\s+and\s+confidential\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\battorney[\-\s]client\s+privilege\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\bsubject\s+to\s+privilege\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\blegal\s+advice\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\battorney\s+work\s+product\b`)},

	// Attorney email domain patterns in To/From context
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)(?:to|from|cc|bcc)[\s:]+[^\s]+@[a-z]+\.law\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)(?:to|from|cc|bcc)[\s:]+[^\s]+@[a-z]+llp\.com\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)(?:to|from|cc|bcc)[\s:]+[^\s]+@[a-z]+legalgroup\.com\b`)},

	// Explicit label prefixes used in email subject lines and document headers
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`\bPRIVILEGED\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\bCONFIDENTIAL\s*[-–]\s*ATTORNEY\b`)},
	{PrivilegeTypeAttorneyClient, regexp.MustCompile(`(?i)\bACP:\s`)},

	// --- Work Product markers ---

	{PrivilegeTypeWorkProduct, regexp.MustCompile(`(?i)\bprepared\s+in\s+anticipation\s+of\s+litigation\b`)},
	{PrivilegeTypeWorkProduct, regexp.MustCompile(`(?i)\bwork\s+product\b`)},
	{PrivilegeTypeWorkProduct, regexp.MustCompile(`(?i)\bpreserve.*?(?:litigation|legal\s+hold)\b`)},

	// --- Litigation Hold markers ---

	{PrivilegeTypeLitigationHold, regexp.MustCompile(`(?i)\blitigation\s+hold\b`)},
	{PrivilegeTypeLitigationHold, regexp.MustCompile(`(?i)\blegal\s+hold\s+notice\b`)},
	{PrivilegeTypeLitigationHold, regexp.MustCompile(`(?i)\bpreserve\s+all\b.*?\b(?:documents?|emails?|records?)\b`)},
}

// ScanForPrivilege returns detected privilege types for the given text.
// An empty slice means no privilege signals were detected.
func ScanForPrivilege(text string) []PrivilegeType {
	var found []PrivilegeType
	seen := make(map[PrivilegeType]bool)
	_ = strings.ToLower // patterns use (?i); referenced to satisfy import

	for _, rule := range privilegePatterns {
		if rule.re.MatchString(text) && !seen[rule.privilegeType] {
			found = append(found, rule.privilegeType)
			seen[rule.privilegeType] = true
		}
	}
	return found
}

// NewLegalPrivilegeMiddleware creates a non-blocking middleware that detects
// attorney-client privilege and related legal doctrine signals in the request body.
//
// On detection it:
//   - sets c.Locals("privilege_detected", true)
//   - sets c.Locals("privilege_types", []PrivilegeType{...})
//   - adds an X-Legal-Privilege response header listing the detected types
//   - sends a SIEMEvent{EventType: "privilege_detected"} to siemChan (non-blocking)
//
// The middleware never blocks the request; routing decisions are left to the caller.
func NewLegalPrivilegeMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		detected := ScanForPrivilege(body)
		if len(detected) == 0 {
			return c.Next()
		}

		c.Locals("privilege_detected", true)
		c.Locals("privilege_types", detected)

		// Build comma-separated type string for the response header.
		typeStrs := make([]string, len(detected))
		for i, pt := range detected {
			typeStrs[i] = string(pt)
		}
		headerVal := strings.Join(typeStrs, ",")
		c.Set("X-Legal-Privilege", headerVal)

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}

			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "privilege_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "privilege_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":         uid,
						"privilege_types": typeStrs,
						"path":            c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return c.Next()
	}
}
