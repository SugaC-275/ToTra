package handlers

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// BundleComplianceChecker queries which compliance bundles are active for a tenant.
type BundleComplianceChecker interface {
	GetActiveBundleIDs(ctx context.Context, tenantID string) ([]string, error)
}

// CheckBundleCompliance enforces provider-level requirements for active compliance bundles.
// Returns non-nil error (and writes a 451 response) when the selected model does not
// satisfy the requirements of an active bundle. Callers must return immediately on error.
//
// When the UserInfo carries bundle_ids (set by SAML SSO JWT), those are used directly
// for per-user compliance enforcement without a DB query. Otherwise the tenant-level
// active bundles are fetched from the database.
func CheckBundleCompliance(c *fiber.Ctx, user *middleware.UserInfo, modelCfg *storage.ModelConfig, checker BundleComplianceChecker) error {
	if checker == nil || user == nil || modelCfg == nil {
		return nil
	}

	var bundleIDs []string
	if len(user.BundleIDs) > 0 {
		// Per-user bundle assignment from SAML JWT — takes priority over tenant-level bundles.
		bundleIDs = user.BundleIDs
	} else {
		var err error
		bundleIDs, err = checker.GetActiveBundleIDs(c.Context(), user.TenantID)
		if err != nil || len(bundleIDs) == 0 {
			// Non-fatal: if the query fails we degrade gracefully rather than blocking traffic.
			return nil
		}
	}

	for _, bid := range bundleIDs {
		switch bid {
		case "healthcare":
			if !modelCfg.HIPAAEligible {
				return c.Status(fiber.StatusUnavailableForLegalReasons).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "Healthcare compliance bundle is active: the selected model must be marked HIPAA-eligible. " +
							"Set hipaa_eligible=true on the model config or choose a HIPAA-eligible model.",
						"type":   "compliance_error",
						"bundle": "healthcare",
					},
				})
			}
		case "government":
			if !modelCfg.GovCloud {
				return c.Status(fiber.StatusUnavailableForLegalReasons).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "Government compliance bundle is active: the selected model must use a FedRAMP-authorized GovCloud endpoint. " +
							"Set govcloud=true on the model config or choose a GovCloud model.",
						"type":   "compliance_error",
						"bundle": "government",
					},
				})
			}
		case "legal":
			if modelCfg.DataRegion == "" {
				return c.Status(fiber.StatusUnavailableForLegalReasons).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "Legal compliance bundle is active: the selected model must have a data region configured " +
							"to enforce data residency requirements. Set data_region (e.g. 'us', 'eu') on the model config.",
						"type":   "compliance_error",
						"bundle": "legal",
					},
				})
			}
		}
	}
	return nil
}
