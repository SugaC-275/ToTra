package middleware

import (
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// EEOCBiasType identifies a category of EEOC-protected characteristic.
type EEOCBiasType string

const (
	BiasGender         EEOCBiasType = "gender"
	BiasAge            EEOCBiasType = "age"
	BiasRace           EEOCBiasType = "race_ethnicity"
	BiasDisability     EEOCBiasType = "disability"
	BiasReligion       EEOCBiasType = "religion"
	BiasNationalOrigin EEOCBiasType = "national_origin"
	BiasGeneticInfo    EEOCBiasType = "genetic_information"
)

// hiringContextRe matches request bodies that are in an employment/HR context.
var hiringContextRe = regexp.MustCompile(
	`(?i)\b(?:job\s+(?:description|posting|listing|application)|candidate|hire|hiring|recruit(?:ment|ing|er)?|applicant|position|vacancy|opening|employment|workforce|onboard)\b`,
)

type eeocRule struct {
	biasType EEOCBiasType
	re       *regexp.Regexp
}

var eeocPatterns = []*eeocRule{
	// --- Gender bias ---
	// Gendered job titles / bro-culture dog-whistles
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\b(?:manpower|manning|manhole)\b`),
	},
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\b(?:stewardess|fireman|policeman|congressman|chairwoman|chairman)\b`),
	},
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\b(?:rockstar|ninja|guru)\b`),
	},
	// "aggressive" as candidate trait penalises women
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\baggressive\s+(?:candidate|applicant|professional|go-getter|self-starter)\b`),
	},
	// Gendered pronoun defaults for generic candidate
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\b(?:he\s+will|his\s+role|she\s+must|her\s+role)\b`),
	},
	// "he or she" (preferred: they)
	{
		biasType: BiasGender,
		re:       regexp.MustCompile(`(?i)\bhe\s+or\s+she\b`),
	},

	// --- Age bias ---
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\brecent\s+graduate(?:s)?\b`),
	},
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\bdigital\s+native(?:s)?\b`),
	},
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\byoung\s+(?:professional|candidate|talent|energetic)\b`),
	},
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\benergetic\s+(?:candidate|applicant|professional|young)\b`),
	},
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\boverqualified\b`),
	},
	// Explicit age caps
	{
		biasType: BiasAge,
		re:       regexp.MustCompile(`(?i)\b(?:must\s+be\s+under\s+\d+|maximum\s+age|age\s+limit(?::\s*\d+)?|age\s+cap)\b`),
	},

	// --- Race / Ethnicity bias ---
	// Neighbourhood-descriptor steering used in candidate context
	{
		biasType: BiasRace,
		re:       regexp.MustCompile(`(?i)\b(?:good\s+schools|safe\s+neighborhood|up[\s-]and[\s-]coming\s+area)\b`),
	},
	// Homogeneity-coded "cultural fit" without job-relevant qualifier
	{
		biasType: BiasRace,
		re:       regexp.MustCompile(`(?i)\bcultural\s+fit\b`),
	},

	// --- Disability bias ---
	{
		biasType: BiasDisability,
		re:       regexp.MustCompile(`(?i)\bable[\s-]bodied\b`),
	},
	{
		biasType: BiasDisability,
		re:       regexp.MustCompile(`(?i)\bno\s+disabilities?\b`),
	},
	// "physically fit" without safety-critical framing
	{
		biasType: BiasDisability,
		re:       regexp.MustCompile(`(?i)\bphysically\s+fit\s+(?:candidate|applicant|required|preferred|only)\b`),
	},
	// Must stand all day without ADA accommodation note
	{
		biasType: BiasDisability,
		re:       regexp.MustCompile(`(?i)\bmust\s+be\s+able\s+to\s+stand\s+(?:all\s+day|for\s+(?:long|extended)\s+periods?)(?:\s+without)?\b`),
	},

	// --- Religion bias ---
	{
		biasType: BiasReligion,
		re:       regexp.MustCompile(`(?i)\b(?:christian[\s-]only|must\s+be\s+(?:christian|muslim|jewish|catholic|protestant|hindu|buddhist)|no\s+(?:muslims?|jews?|christians?|atheists?))\b`),
	},

	// --- National origin bias ---
	{
		biasType: BiasNationalOrigin,
		re:       regexp.MustCompile(`(?i)\b(?:native[\s-]born|born\s+in\s+(?:the\s+)?(?:us|usa|america|uk|england)|no\s+(?:immigrants?|foreigners?|non[\s-]citizens?))\b`),
	},
	// "must speak English only" signals
	{
		biasType: BiasNationalOrigin,
		re:       regexp.MustCompile(`(?i)\benglish[\s-]only\s+(?:workplace|environment|required|candidates?|speakers?)\b`),
	},

	// --- Genetic information bias ---
	{
		biasType: BiasGeneticInfo,
		re:       regexp.MustCompile(`(?i)\b(?:genetic\s+test(?:ing)?|family\s+medical\s+history\s+required|dna\s+test(?:ing)?|genomic\s+screening)\b`),
	},
}

// ScanForEEOCBias returns the distinct EEOC bias types detected in text.
// Returns an empty slice when no bias signals are found.
func ScanForEEOCBias(text string) []EEOCBiasType {
	lower := strings.ToLower(text)
	_ = lower // patterns use (?i); kept for future keyword-only checks

	seen := make(map[EEOCBiasType]struct{})
	for _, rule := range eeocPatterns {
		if rule.re.MatchString(text) {
			seen[rule.biasType] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return []EEOCBiasType{}
	}

	result := make([]EEOCBiasType, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	return result
}

// NewEEOCBiasMiddleware returns a Fiber middleware that detects EEOC bias
// signals in request bodies. On detection it:
//   - Sets c.Locals("eeoc_bias_detected", true)
//   - Sets c.Locals("eeoc_bias_types", []EEOCBiasType{...})
//   - Sets c.Locals("nyc_ll144_applicable", true) when a hiring context is detected
//   - Adds the X-EEOC-Bias-Signal response header listing detected bias types
//   - Sends a non-blocking SIEMEvent with EventType "eeoc_bias_detected"
//
// The middleware does NOT block requests; enforcement is advisory (human review).
func NewEEOCBiasMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		types := ScanForEEOCBias(body)

		if len(types) == 0 {
			return c.Next()
		}

		c.Locals("eeoc_bias_detected", true)
		c.Locals("eeoc_bias_types", types)

		// NYC Local Law 144: algorithmic hiring tools require bias audits.
		// Signal when bias detected in a hiring/recruitment context.
		if hiringContextRe.MatchString(body) {
			c.Locals("nyc_ll144_applicable", true)
		}

		typeStrs := make([]string, len(types))
		for i, t := range types {
			typeStrs[i] = string(t)
		}
		c.Set("X-EEOC-Bias-Signal", strings.Join(typeStrs, ","))

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
				EventType: "eeoc_bias_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "eeoc_bias_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":    uid,
						"bias_types": typeStrs,
						"action":     "flagged",
						"path":       c.Path(),
					},
				},
			}:
			default: // drop if channel is full
			}
		}

		return c.Next()
	}
}
