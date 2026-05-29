package middleware

import "github.com/gofiber/fiber/v2"

// FedRAMPLevel represents the FedRAMP authorization level of a model deployment.
type FedRAMPLevel string

const (
	FedRAMPLow      FedRAMPLevel = "low"
	FedRAMPModerate FedRAMPLevel = "moderate"
	FedRAMPHigh     FedRAMPLevel = "high"
	FedRAMPGovCloud FedRAMPLevel = "govcloud"
)

// fedRAMPOrder maps each level to a numeric rank for comparison.
// Higher number = stricter authorization.
var fedRAMPOrder = map[FedRAMPLevel]int{
	"":              0,
	FedRAMPLow:      1,
	FedRAMPModerate: 2,
	FedRAMPHigh:     3,
	FedRAMPGovCloud: 4,
}

// RequiredFedRAMPLevel returns the minimum FedRAMP level required for the
// current request based on CUI categories stored in context locals.
//
// Mapping:
//
//	export_control present → High
//	law_enforcement present → Moderate
//	any other CUI          → Low
//	no CUI detected        → "" (no requirement)
func RequiredFedRAMPLevel(c *fiber.Ctx) FedRAMPLevel {
	detected, _ := c.Locals("cui_detected").(bool)
	if !detected {
		return ""
	}

	categories, _ := c.Locals("cui_categories").([]CUICategory)
	for _, cat := range categories {
		if cat == CUIExport {
			return FedRAMPHigh
		}
	}
	for _, cat := range categories {
		if cat == CUILawEnforcement {
			return FedRAMPModerate
		}
	}
	if len(categories) > 0 {
		return FedRAMPLow
	}
	return ""
}

// FedRAMPLevelSatisfied returns true when modelLevel meets or exceeds required.
// Level order: govcloud(4) > high(3) > moderate(2) > low(1) > ""(0).
// A model with no configured level ("") never satisfies any requirement.
func FedRAMPLevelSatisfied(modelLevel FedRAMPLevel, required FedRAMPLevel) bool {
	if required == "" {
		return true
	}
	modelRank, modelKnown := fedRAMPOrder[modelLevel]
	if !modelKnown {
		return false
	}
	requiredRank, requiredKnown := fedRAMPOrder[required]
	if !requiredKnown {
		return false
	}
	return modelRank >= requiredRank
}

// FedRAMPComplianceError returns the HTTP 451 error body used when CUI content
// is routed to a model that does not meet the required FedRAMP authorization.
func FedRAMPComplianceError(required FedRAMPLevel) fiber.Map {
	return fiber.Map{
		"error": fiber.Map{
			"message": "request blocked: CUI content requires FedRAMP " + string(required) + " or higher authorization",
			"type":    "fedramp_compliance_violation",
			"required_fedramp_level": string(required),
		},
	}
}
