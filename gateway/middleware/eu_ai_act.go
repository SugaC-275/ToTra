package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// AIActRiskLevel classifies EU AI Act Regulation 2024/1689 risk tiers.
type AIActRiskLevel string

const (
	AIActMinimal      AIActRiskLevel = "minimal"      // spam filters, recommendations
	AIActLimited      AIActRiskLevel = "limited"      // chatbots (must disclose AI), deepfakes
	AIActHigh         AIActRiskLevel = "high"         // employment, credit, healthcare, law enforcement, education
	AIActUnacceptable AIActRiskLevel = "unacceptable" // social scoring, manipulation, covert biometric
)

type aiActRule struct {
	level  AIActRiskLevel
	reason string
	re     *regexp.Regexp
}

// unacceptableRules covers Art. 5 prohibited AI practices.
var unacceptableRules = []*aiActRule{
	{
		AIActUnacceptable,
		"social scoring system",
		regexp.MustCompile(`(?i)(?:social\s+credit\s+score|citizen\s+score|behavior\s+score|social\s+scoring)`),
	},
	{
		AIActUnacceptable,
		"subliminal manipulation",
		regexp.MustCompile(`(?i)(?:subliminal|exploit\s+vulnerabilit|manipulate\s+behavior).*(?:unconscious|subconscious)|(?:unconscious|subconscious).*(?:subliminal|exploit\s+vulnerabilit|manipulate\s+behavior)`),
	},
	{
		AIActUnacceptable,
		"real-time biometric surveillance in public spaces",
		regexp.MustCompile(`(?i)(?:real[\-\s]time\s+biometric|live\s+facial\s+recognition).*(?:public\s+space|public\s+area|street|crowd)|(?:public\s+space|public\s+area|street|crowd).*(?:real[\-\s]time\s+biometric|live\s+facial\s+recognition)`),
	},
}

// highRiskRules covers Art. 6 + Annex III use cases requiring oversight.
var highRiskRules = []*aiActRule{
	{
		AIActHigh,
		"employment decision support",
		regexp.MustCompile(`(?i)(?:hiring\s+decision|recruitment\s+screening|job\s+applicant|employee\s+monitoring|performance\s+evaluation)`),
	},
	{
		AIActHigh,
		"credit or financial risk assessment",
		regexp.MustCompile(`(?i)(?:credit\s+scor(?:ing|e)|creditworthiness|loan\s+decision|insurance\s+risk\s+scor(?:ing|e))`),
	},
	{
		AIActHigh,
		"healthcare clinical decision support",
		regexp.MustCompile(`(?i)(?:medical\s+diagnosis|clinical\s+decision|patient\s+triage|treatment\s+recommendation)`),
	},
	{
		AIActHigh,
		"law enforcement risk assessment",
		regexp.MustCompile(`(?i)(?:recidivism|criminal\s+risk|policing|threat\s+assessment)`),
	},
	{
		AIActHigh,
		"education assessment or proctoring",
		regexp.MustCompile(`(?i)(?:student\s+assessment|exam\s+proctoring|educational\s+outcome|academic\s+scor(?:ing|e))`),
	},
	{
		AIActHigh,
		"critical infrastructure management",
		regexp.MustCompile(`(?i)(?:grid\s+management|traffic\s+control|water\s+treatment).*(?:ai|automat(?:ed|ic|ion))|(?:ai|automat(?:ed|ic|ion)).*(?:grid\s+management|traffic\s+control|water\s+treatment)`),
	},
}

// limitedRules covers Art. 52 transparency obligations.
var limitedRules = []*aiActRule{
	{
		AIActLimited,
		"chatbot or virtual assistant interaction",
		regexp.MustCompile(`(?i)(?:chatbot|virtual\s+assistant|customer\s+service\s+bot)`),
	},
	{
		AIActLimited,
		"synthetic or deepfake media generation",
		regexp.MustCompile(`(?i)(?:generate\s+image\s+of\s+person|deepfake|synthetic\s+voice|voice\s+clon(?:ing|e))`),
	},
}

// AIActAssessment is the result of a risk classification.
type AIActAssessment struct {
	RiskLevel AIActRiskLevel
	Reasons   []string
}

// ClassifyEUAIActRisk analyzes request body text and returns a risk assessment.
// Rules are evaluated in descending severity; the highest matching tier wins.
func ClassifyEUAIActRisk(text string) AIActAssessment {
	lower := strings.ToLower(text)
	_ = lower // regexp flags handle case via (?i)

	for _, rule := range unacceptableRules {
		if rule.re.MatchString(text) {
			return AIActAssessment{RiskLevel: AIActUnacceptable, Reasons: []string{rule.reason}}
		}
	}

	var highReasons []string
	for _, rule := range highRiskRules {
		if rule.re.MatchString(text) {
			highReasons = append(highReasons, rule.reason)
		}
	}
	if len(highReasons) > 0 {
		return AIActAssessment{RiskLevel: AIActHigh, Reasons: highReasons}
	}

	for _, rule := range limitedRules {
		if rule.re.MatchString(text) {
			return AIActAssessment{RiskLevel: AIActLimited, Reasons: []string{rule.reason}}
		}
	}

	return AIActAssessment{RiskLevel: AIActMinimal, Reasons: []string{"no high-risk patterns detected"}}
}

// requestHash returns a truncated SHA256 hex of the body for the oversight log.
func requestHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:16]
}

// NewEUAIActMiddleware enforces EU AI Act Regulation 2024/1689:
//
//  1. Classifies risk from the request body.
//  2. Sets c.Locals("eu_ai_act_risk") and c.Locals("eu_ai_act_reasons").
//  3. Blocks "unacceptable" requests with HTTP 403.
//  4. For "high": sets c.Locals("require_human_oversight", true) and emits a SIEM event.
//  5. For "limited": sets X-AI-Disclosure response header.
//  6. All levels: sets X-EU-AI-Act-Risk response header.
func NewEUAIActMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		assessment := ClassifyEUAIActRisk(string(body))

		c.Locals("eu_ai_act_risk", assessment.RiskLevel)
		c.Locals("eu_ai_act_reasons", assessment.Reasons)

		// Always advertise the risk level to downstream handlers / response.
		c.Set("X-EU-AI-Act-Risk", string(assessment.RiskLevel))

		switch assessment.RiskLevel {
		case AIActUnacceptable:
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": fiber.Map{
					"message":    "Request blocked: EU AI Act Art. 5 prohibits this AI use case",
					"type":      "eu_ai_act_prohibited",
					"risk_level": "unacceptable",
				},
			})

		case AIActHigh:
			c.Locals("require_human_oversight", true)

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
					EventType: "eu_ai_act_high_risk",
					Payload: map[string]any{
						"source":      "totra",
						"tenant_id":   tid,
						"event_type":  "eu_ai_act_high_risk",
						"occurred_at": time.Now().UTC().Format(time.RFC3339),
						"detail": map[string]any{
							"user_id":      uid,
							"risk_level":   string(AIActHigh),
							"risk_reasons": assessment.Reasons,
							"request_hash": requestHash(body),
							"path":         c.Path(),
						},
					},
				}:
				default: // drop if channel full
				}
			}

		case AIActLimited:
			c.Set("X-AI-Disclosure", "This response is generated by AI")
		}

		return c.Next()
	}
}
