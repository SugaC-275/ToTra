package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

// OIDCConfig holds provider configuration for a tenant.
type OIDCConfig struct {
	TenantID     string
	Issuer       string
	ClientID     string
	ClientSecret string // plaintext in memory, encrypted at rest
	RedirectURI  string
	Enabled      bool
}

// oidcDiscovery is the subset of fields we need from .well-known/openid-configuration.
type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// stateEntry tracks an OIDC state value with expiry.
type stateEntry struct {
	tenantID  string
	expiresAt time.Time
}

// OIDCService handles OIDC config persistence and provider interactions.
type OIDCService struct {
	pool       *pgxpool.Pool
	encKey     string // hex-encoded 32-byte key
	stateMu    sync.Mutex
	stateStore map[string]stateEntry
	httpClient *http.Client
}

// NewOIDCService creates an OIDCService. encKeyHex must be a 64-char hex string (32 bytes).
func NewOIDCService(pool *pgxpool.Pool, encKeyHex string) *OIDCService {
	return &OIDCService{
		pool:       pool,
		encKey:     encKeyHex,
		stateStore: make(map[string]stateEntry),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetConfig retrieves and decrypts the OIDC config for a tenant.
func (s *OIDCService) GetConfig(ctx context.Context, tenantID string) (*OIDCConfig, error) {
	var cfg OIDCConfig
	var secretEnc string
	err := s.pool.QueryRow(ctx,
		`SELECT tenant_id, issuer, client_id, client_secret_enc, redirect_uri, enabled
		 FROM oidc_configs WHERE tenant_id = $1`,
		tenantID,
	).Scan(&cfg.TenantID, &cfg.Issuer, &cfg.ClientID, &secretEnc, &cfg.RedirectURI, &cfg.Enabled)
	if err != nil {
		return nil, fmt.Errorf("oidc config not found: %w", err)
	}
	plain, err := crypto.Decrypt(secretEnc, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt client secret: %w", err)
	}
	cfg.ClientSecret = plain
	return &cfg, nil
}

// SetConfig upserts the OIDC config, encrypting the client secret.
func (s *OIDCService) SetConfig(ctx context.Context, cfg *OIDCConfig) error {
	if cfg.TenantID == "" || cfg.Issuer == "" || cfg.ClientID == "" ||
		cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return fmt.Errorf("all config fields are required")
	}
	enc, err := crypto.Encrypt(cfg.ClientSecret, s.encKey)
	if err != nil {
		return fmt.Errorf("encrypt client secret: %w", err)
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO oidc_configs (tenant_id, issuer, client_id, client_secret_enc, redirect_uri, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id) DO UPDATE SET
		   issuer = EXCLUDED.issuer,
		   client_id = EXCLUDED.client_id,
		   client_secret_enc = EXCLUDED.client_secret_enc,
		   redirect_uri = EXCLUDED.redirect_uri,
		   enabled = EXCLUDED.enabled,
		   updated_at = NOW()`,
		cfg.TenantID, cfg.Issuer, cfg.ClientID, enc, cfg.RedirectURI, cfg.Enabled,
	)
	return err
}

// TestConnection verifies the issuer is reachable by fetching its discovery document.
func (s *OIDCService) TestConnection(ctx context.Context, issuer string) error {
	_, err := s.FetchDiscovery(ctx, issuer)
	return err
}

// FetchDiscovery fetches the OIDC discovery document from the issuer.
func (s *OIDCService) FetchDiscovery(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	wellKnown := strings.TrimRight(issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery returned %d", resp.StatusCode)
	}
	var doc oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery document missing required endpoints")
	}
	return &doc, nil
}

// GenerateAuthURL builds the authorization redirect URL and stores state.
func (s *OIDCService) GenerateAuthURL(ctx context.Context, cfg *OIDCConfig, tenantID string) (string, error) {
	disc, err := s.FetchDiscovery(ctx, cfg.Issuer)
	if err != nil {
		return "", err
	}
	state := s.storeState(tenantID)
	params := url.Values{
		"response_type": {"code"},
		"client_id":     {cfg.ClientID},
		"redirect_uri":  {cfg.RedirectURI},
		"scope":         {"openid email profile"},
		"state":         {state},
	}
	return disc.AuthorizationEndpoint + "?" + params.Encode(), nil
}

// ValidateState checks and consumes a state token; returns the tenantID it was issued for.
func (s *OIDCService) ValidateState(state string) (string, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	entry, ok := s.stateStore[state]
	if !ok {
		return "", fmt.Errorf("invalid state")
	}
	delete(s.stateStore, state)
	if time.Now().After(entry.expiresAt) {
		return "", fmt.Errorf("state expired")
	}
	return entry.tenantID, nil
}

// ExchangeCode exchanges an authorization code for an access token.
// Returns the raw access token string.
func (s *OIDCService) ExchangeCode(ctx context.Context, cfg *OIDCConfig, code string) (string, error) {
	disc, err := s.FetchDiscovery(ctx, cfg.Issuer)
	if err != nil {
		return "", err
	}
	body := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {cfg.RedirectURI},
		"client_id":    {cfg.ClientID},
		"client_secret": {cfg.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, disc.TokenEndpoint,
		strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, raw)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return tokenResp.AccessToken, nil
}

// GetUserInfo calls the userinfo endpoint and returns email and name.
func (s *OIDCService) GetUserInfo(ctx context.Context, cfg *OIDCConfig, accessToken string) (email, name string, err error) {
	disc, err := s.FetchDiscovery(ctx, cfg.Issuer)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, disc.UserinfoEndpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo returned %d", resp.StatusCode)
	}
	var info struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", "", fmt.Errorf("decode userinfo: %w", err)
	}
	if info.Email == "" {
		return "", "", fmt.Errorf("userinfo missing email")
	}
	return info.Email, info.Name, nil
}

// storeState generates a random state token and stores it with a 5-minute TTL.
func (s *OIDCService) storeState(tenantID string) string {
	b := make([]byte, 16)
	rand.Read(b)
	state := hex.EncodeToString(b)
	s.stateMu.Lock()
	s.stateStore[state] = stateEntry{tenantID: tenantID, expiresAt: time.Now().Add(5 * time.Minute)}
	s.stateMu.Unlock()
	s.gcStates()
	return state
}

// gcStates removes expired state entries (called opportunistically).
func (s *OIDCService) gcStates() {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	now := time.Now()
	for k, v := range s.stateStore {
		if now.After(v.expiresAt) {
			delete(s.stateStore, k)
		}
	}
}
