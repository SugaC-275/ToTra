package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FDADataType identifies a category of FDA-regulated or GxP-sensitive data.
type FDADataType string

const (
	FDASubjectData   FDADataType = "clinical_subject" // subject/patient IDs in trials
	FDAAdverseEvent  FDADataType = "adverse_event"    // AE/SAE reports
	FDAProtocol      FDADataType = "protocol"         // study protocols (IND/NDA refs)
	FDABatchRecord   FDADataType = "batch_record"     // manufacturing batch records
	FDAGxPProcess    FDADataType = "gxp_process"      // GMP/GCP/GLP context
	FDARegulatoryDoc FDADataType = "regulatory_doc"   // 510k, BLA, IND, NDA, ANDA
)

type fdaRule struct {
	dataType FDADataType
	re       *regexp.Regexp
}

var fdaPatterns = []*fdaRule{
	// --- Clinical subject identifiers ---
	// Trial subject format: Subject AB1-001234
	{FDASubjectData, regexp.MustCompile(`(?i)\bSubject\s+[A-Z0-9]{2,10}-\d{3,6}\b`)},
	// Patient / participant ID keyword
	{FDASubjectData, regexp.MustCompile(`(?i)(?:Patient\s+ID|Participant\s+ID)[\s:]*[A-Z0-9\-]{3,20}`)},
	// Randomization / enrollment number
	{FDASubjectData, regexp.MustCompile(`(?i)(?:randomization\s+number|enrollment\s+number)`)},

	// --- Adverse events ---
	// AE / SAE in clinical context (require surrounding word boundary)
	{FDAAdverseEvent, regexp.MustCompile(`(?i)\badverse\s+event\b`)},
	{FDAAdverseEvent, regexp.MustCompile(`(?i)\bserious\s+adverse\s+event\b`)},
	{FDAAdverseEvent, regexp.MustCompile(`\b(?:SAE|CIOMS|MedDRA|VAERS)\b`)},
	{FDAAdverseEvent, regexp.MustCompile(`(?i)\bcausal\s+relationship\b`)},

	// --- Regulatory submissions ---
	{FDARegulatoryDoc, regexp.MustCompile(`\bIND\s+\d{6}\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`\bNDA\s+\d{6}\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`\bBLA\s+\d{6}\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`\bANDA\s+\d{6}\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`(?i)510\(k\)`)},
	{FDARegulatoryDoc, regexp.MustCompile(`\bPMA\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`(?i)\binvestigational\s+new\s+drug\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`(?i)\bnew\s+drug\s+application\b`)},
	{FDARegulatoryDoc, regexp.MustCompile(`(?i)\bbiologics\s+license\b`)},

	// --- GxP process markers ---
	{FDAGxPProcess, regexp.MustCompile(`(?i)\bgood\s+manufacturing\s+practice\b`)},
	{FDAGxPProcess, regexp.MustCompile(`(?i)\b(?:GMP|GCP|GLP|GDP)\b`)},
	{FDAGxPProcess, regexp.MustCompile(`(?i)\bgood\s+clinical\s+practice\b`)},
	{FDAGxPProcess, regexp.MustCompile(`(?i)\bgood\s+laboratory\s+practice\b`)},
	{FDAGxPProcess, regexp.MustCompile(`(?i)\bdeviation\s+report\b`)},
	{FDAGxPProcess, regexp.MustCompile(`(?i)\bCAPA\b`)},

	// --- Batch records ---
	{FDABatchRecord, regexp.MustCompile(`(?i)\bbatch\s+record\b`)},
	{FDABatchRecord, regexp.MustCompile(`(?i)\bmaster\s+batch\s+record\b`)},

	// --- Study protocols ---
	{FDAProtocol, regexp.MustCompile(`(?i)\bstudy\s+protocol\b`)},
	{FDAProtocol, regexp.MustCompile(`(?i)\bclinical\s+protocol\b`)},
}

// ScanForFDAData returns the distinct FDA/GxP data types detected in text.
// Returns an empty slice when no regulated content is found.
func ScanForFDAData(text string) []FDADataType {
	seen := make(map[FDADataType]struct{})
	lower := strings.ToLower(text)
	_ = lower // patterns use (?i); kept for potential future keyword checks

	for _, rule := range fdaPatterns {
		if rule.re.MatchString(text) {
			seen[rule.dataType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []FDADataType{}
	}

	result := make([]FDADataType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewPharmaFDAMiddleware returns a Fiber middleware that detects FDA 21 CFR
// Part 11 / GxP-regulated data in request bodies. On detection it:
//   - Sets c.Locals("fda_data_detected", true)
//   - Sets c.Locals("fda_data_types", []FDADataType{...})
//   - Adds the X-FDA-21CFR-Signal: true response header
//   - Sends a non-blocking SIEMEvent with EventType "fda_data_detected"
//
// The middleware does NOT block the request; enforcement is left to upstream policy.
func NewPharmaFDAMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForFDAData(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("fda_data_detected", true)
		c.Locals("fda_data_types", types)
		c.Set("X-FDA-21CFR-Signal", "true")

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
				EventType: "fda_data_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "fda_data_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":    uid,
						"fda_types":  typeStrs,
						"path":       c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return c.Next()
	}
}
