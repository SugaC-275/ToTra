package services

// GuardrailConfig mirrors storage.GuardrailConfig for the admin service layer.
// It is defined here so the admin package has a typed representation without
// importing the gateway/storage package.
type GuardrailConfig struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Name         string         `json:"name"`       // "pii_scan" | "injection_detect" | "policy_rules" | "response_pii"
	Enabled      bool           `json:"enabled"`
	Strictness   string         `json:"strictness"` // "permissive" | "standard" | "strict"
	CustomConfig map[string]any `json:"custom_config"`
	BundleID     string         `json:"bundle_id,omitempty"`
}
