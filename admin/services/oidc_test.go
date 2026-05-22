package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/crypto"
)

const testEncKey = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

// newTestSvc returns an OIDCService wired to the given http.Client (for mocking HTTP calls).
func newTestSvc(client *http.Client) *OIDCService {
	return &OIDCService{
		encKey:     testEncKey,
		stateStore: make(map[string]stateEntry),
		httpClient: client,
	}
}

// ---------------------------------------------------------------------------
// FetchDiscovery
// ---------------------------------------------------------------------------

func TestFetchDiscovery_OK(t *testing.T) {
	doc := map[string]string{
		"authorization_endpoint": "https://provider/auth",
		"token_endpoint":         "https://provider/token",
		"userinfo_endpoint":      "https://provider/userinfo",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/.well-known/openid-configuration", r.URL.Path)
		json.NewEncoder(w).Encode(doc)
	}))
	defer srv.Close()

	svc := newTestSvc(srv.Client())
	disc, err := svc.FetchDiscovery(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "https://provider/auth", disc.AuthorizationEndpoint)
	assert.Equal(t, "https://provider/token", disc.TokenEndpoint)
}

func TestFetchDiscovery_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	svc := newTestSvc(srv.Client())
	_, err := svc.FetchDiscovery(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestFetchDiscovery_MissingEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"issuer": "x"})
	}))
	defer srv.Close()

	svc := newTestSvc(srv.Client())
	_, err := svc.FetchDiscovery(context.Background(), srv.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required endpoints")
}

// ---------------------------------------------------------------------------
// State management
// ---------------------------------------------------------------------------

func TestStateRoundTrip(t *testing.T) {
	svc := &OIDCService{stateStore: make(map[string]stateEntry)}
	state := svc.storeState("tenant-abc")
	assert.NotEmpty(t, state)

	tid, err := svc.ValidateState(state)
	require.NoError(t, err)
	assert.Equal(t, "tenant-abc", tid)

	// Second call should fail — state is consumed after first use.
	_, err = svc.ValidateState(state)
	require.Error(t, err)
}

func TestStateExpired(t *testing.T) {
	svc := &OIDCService{stateStore: make(map[string]stateEntry)}
	svc.stateStore["expired-state"] = stateEntry{
		tenantID:  "t1",
		expiresAt: time.Now().Add(-1 * time.Minute),
	}
	_, err := svc.ValidateState("expired-state")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestValidateState_Unknown(t *testing.T) {
	svc := &OIDCService{stateStore: make(map[string]stateEntry)}
	_, err := svc.ValidateState("unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state")
}

// ---------------------------------------------------------------------------
// ExchangeCode
// ---------------------------------------------------------------------------

func TestExchangeCode_OK(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": base + "/auth",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		r.ParseForm()
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "mycode", r.FormValue("code"))
		json.NewEncoder(w).Encode(map[string]string{"access_token": "tok123"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	svc := newTestSvc(srv.Client())
	cfg := &OIDCConfig{
		Issuer:       srv.URL,
		ClientID:     "cid",
		ClientSecret: "secret",
		RedirectURI:  "http://app/callback",
	}
	tok, err := svc.ExchangeCode(context.Background(), cfg, "mycode")
	require.NoError(t, err)
	assert.Equal(t, "tok123", tok)
}

func TestExchangeCode_NoAccessToken(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": base + "/auth",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id_token": "only"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	svc := newTestSvc(srv.Client())
	cfg := &OIDCConfig{Issuer: srv.URL, ClientID: "c", ClientSecret: "s", RedirectURI: "u"}
	_, err := svc.ExchangeCode(context.Background(), cfg, "code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no access_token")
}

// ---------------------------------------------------------------------------
// GetUserInfo
// ---------------------------------------------------------------------------

func TestGetUserInfo_OK(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": base + "/auth",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer mytoken", r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode(map[string]string{"email": "alice@acme.com", "name": "Alice"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	svc := newTestSvc(srv.Client())
	cfg := &OIDCConfig{Issuer: srv.URL}
	email, name, err := svc.GetUserInfo(context.Background(), cfg, "mytoken")
	require.NoError(t, err)
	assert.Equal(t, "alice@acme.com", email)
	assert.Equal(t, "Alice", name)
}

func TestGetUserInfo_MissingEmail(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": base + "/auth",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"name": "Bob"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL

	svc := newTestSvc(srv.Client())
	cfg := &OIDCConfig{Issuer: srv.URL}
	_, _, err := svc.GetUserInfo(context.Background(), cfg, "tok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing email")
}

// ---------------------------------------------------------------------------
// Encrypt/Decrypt roundtrip used by SetConfig/GetConfig
// ---------------------------------------------------------------------------

func TestEncryptDecryptRoundtrip(t *testing.T) {
	plain := "super-secret-client-secret"
	enc, err := crypto.Encrypt(plain, testEncKey)
	require.NoError(t, err)
	assert.NotEmpty(t, enc)
	assert.NotEqual(t, plain, enc)

	dec, err := crypto.Decrypt(enc, testEncKey)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

// ---------------------------------------------------------------------------
// TestConnection
// ---------------------------------------------------------------------------

func TestTestConnection_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "https://p/auth",
			"token_endpoint":         "https://p/token",
		})
	}))
	defer srv.Close()

	svc := newTestSvc(srv.Client())
	err := svc.TestConnection(context.Background(), srv.URL)
	require.NoError(t, err)
}

func TestTestConnection_Fail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	svc := newTestSvc(srv.Client())
	err := svc.TestConnection(context.Background(), srv.URL)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SetConfig validation
// ---------------------------------------------------------------------------

func TestSetConfig_Validation(t *testing.T) {
	svc := &OIDCService{encKey: testEncKey, stateStore: make(map[string]stateEntry)}
	err := svc.SetConfig(context.Background(), &OIDCConfig{TenantID: "t"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required")
}
