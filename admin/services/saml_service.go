package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SAMLConfig holds IdP configuration for a tenant.
type SAMLConfig struct {
	ID             string            `json:"id"`
	TenantID       string            `json:"tenant_id"`
	EntityID       string            `json:"entity_id"`
	IDPMetadataURL string            `json:"idp_metadata_url"`
	IDPMetadataXML string            `json:"idp_metadata_xml"`
	ACSURL         string            `json:"acs_url"`
	AttributeMap   map[string]string `json:"attribute_map"`
	IsActive       bool              `json:"is_active"`
}

// AttributeBundleRule maps an IdP attribute value to a compliance bundle.
type AttributeBundleRule struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	AttributeName  string `json:"attribute_name"`
	AttributeValue string `json:"attribute_value"`
	BundleID       string `json:"bundle_id"`
}

// SAMLService handles SAML config and attribute-bundle rule persistence.
type SAMLService struct {
	pool *pgxpool.Pool
}

// NewSAMLService creates a SAMLService.
func NewSAMLService(pool *pgxpool.Pool) *SAMLService {
	return &SAMLService{pool: pool}
}

// GetConfig retrieves the SAML config for a tenant.
func (s *SAMLService) GetConfig(ctx context.Context, tenantID string) (*SAMLConfig, error) {
	var cfg SAMLConfig
	var attrMapRaw []byte
	var metaURL, metaXML *string

	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, entity_id, idp_metadata_url, idp_metadata_xml,
		        acs_url, attribute_map, is_active
		 FROM saml_configs WHERE tenant_id = $1`,
		tenantID,
	).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.EntityID,
		&metaURL, &metaXML,
		&cfg.ACSURL, &attrMapRaw, &cfg.IsActive,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("saml config not found for tenant %s", tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("get saml config: %w", err)
	}
	if metaURL != nil {
		cfg.IDPMetadataURL = *metaURL
	}
	if metaXML != nil {
		cfg.IDPMetadataXML = *metaXML
	}
	if err := json.Unmarshal(attrMapRaw, &cfg.AttributeMap); err != nil {
		cfg.AttributeMap = map[string]string{}
	}
	return &cfg, nil
}

// UpsertConfig creates or updates the SAML config for a tenant.
func (s *SAMLService) UpsertConfig(ctx context.Context, cfg *SAMLConfig) error {
	if cfg.TenantID == "" || cfg.EntityID == "" || cfg.ACSURL == "" {
		return fmt.Errorf("tenant_id, entity_id and acs_url are required")
	}
	if cfg.AttributeMap == nil {
		cfg.AttributeMap = map[string]string{}
	}
	attrMapRaw, err := json.Marshal(cfg.AttributeMap)
	if err != nil {
		return fmt.Errorf("marshal attribute_map: %w", err)
	}
	var metaURL, metaXML *string
	if cfg.IDPMetadataURL != "" {
		metaURL = &cfg.IDPMetadataURL
	}
	if cfg.IDPMetadataXML != "" {
		metaXML = &cfg.IDPMetadataXML
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO saml_configs
		   (tenant_id, entity_id, idp_metadata_url, idp_metadata_xml, acs_url, attribute_map, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (tenant_id) DO UPDATE SET
		   entity_id        = EXCLUDED.entity_id,
		   idp_metadata_url = EXCLUDED.idp_metadata_url,
		   idp_metadata_xml = EXCLUDED.idp_metadata_xml,
		   acs_url          = EXCLUDED.acs_url,
		   attribute_map    = EXCLUDED.attribute_map,
		   is_active        = EXCLUDED.is_active`,
		cfg.TenantID, cfg.EntityID, metaURL, metaXML, cfg.ACSURL, attrMapRaw, cfg.IsActive,
	)
	return err
}

// GetAttributeBundleRules returns all attribute-to-bundle rules for a tenant.
func (s *SAMLService) GetAttributeBundleRules(ctx context.Context, tenantID string) ([]AttributeBundleRule, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, attribute_name, attribute_value, bundle_id
		 FROM saml_attribute_bundle_rules
		 WHERE tenant_id = $1
		 ORDER BY attribute_name, attribute_value`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list attribute bundle rules: %w", err)
	}
	defer rows.Close()

	var rules []AttributeBundleRule
	for rows.Next() {
		var r AttributeBundleRule
		if err := rows.Scan(&r.ID, &r.TenantID, &r.AttributeName, &r.AttributeValue, &r.BundleID); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

// UpsertAttributeBundleRule creates or updates an attribute-to-bundle rule.
func (s *SAMLService) UpsertAttributeBundleRule(ctx context.Context, rule *AttributeBundleRule) error {
	if rule.TenantID == "" || rule.AttributeName == "" || rule.AttributeValue == "" || rule.BundleID == "" {
		return fmt.Errorf("tenant_id, attribute_name, attribute_value and bundle_id are required")
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO saml_attribute_bundle_rules
		   (tenant_id, attribute_name, attribute_value, bundle_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (tenant_id, attribute_name, attribute_value) DO UPDATE SET
		   bundle_id = EXCLUDED.bundle_id`,
		rule.TenantID, rule.AttributeName, rule.AttributeValue, rule.BundleID,
	)
	return err
}

// DeleteAttributeBundleRule removes a rule by id, scoped to the tenant.
func (s *SAMLService) DeleteAttributeBundleRule(ctx context.Context, id, tenantID string) error {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM saml_attribute_bundle_rules WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// ResolveBundlesFromAttributes returns the bundle IDs to activate given IdP attributes.
// It looks up all attribute-bundle rules for the tenant and returns matching bundle IDs
// (deduplicated). When no DB rules match, it falls back to the config's attribute_map.
func (s *SAMLService) ResolveBundlesFromAttributes(ctx context.Context, tenantID string, attrs map[string][]string) ([]string, error) {
	rules, err := s.GetAttributeBundleRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var bundleIDs []string

	for _, rule := range rules {
		values, ok := attrs[rule.AttributeName]
		if !ok {
			continue
		}
		for _, v := range values {
			if v == rule.AttributeValue {
				if _, dup := seen[rule.BundleID]; !dup {
					seen[rule.BundleID] = struct{}{}
					bundleIDs = append(bundleIDs, rule.BundleID)
				}
			}
		}
	}
	return bundleIDs, nil
}
