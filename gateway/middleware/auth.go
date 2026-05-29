package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gofiber/fiber/v2"
)

type UserInfo struct {
	UserID       string
	TenantID     string
	Role         string
	DepartmentID string            // optional sub-tenant department; empty when not assigned
	ModelAliases map[string]string // virtual key model aliasing
	// Virtual key governance fields — populated when authenticated via a virtual key.
	BundleIDs []string // compliance bundles active for this key
	PIIPolicy string   // 'inherit' | 'strict' | 'permissive'
}

type UserLookup interface {
	LookupByKeyHash(hash string) (*UserInfo, error)
}

// VirtualKeyLookup is satisfied by *storage.VirtualKeyStore.
// Defined here to avoid an import cycle (storage imports middleware).
type VirtualKeyLookup interface {
	GetByHash(ctx context.Context, keyHash string) (VirtualKeyInfo, error)
}

// VirtualKeyInfo carries the fields needed by auth middleware from a virtual key record.
type VirtualKeyInfo struct {
	ID         string
	TenantID   string
	Name       string
	ModelAlias *string
	PIIPolicy  string
	BundleIDs  []string
}

// samlJWTClaims mirrors the admin JWT Claims struct so we can parse bundle_ids.
type samlJWTClaims struct {
	UserID    string   `json:"uid"`
	TenantID  string   `json:"tid"`
	Role      string   `json:"role"`
	BundleIDs []string `json:"bundle_ids"`
	jwt.RegisteredClaims
}

func NewAuthMiddleware(lookup UserLookup) fiber.Handler {
	return newAuthMiddlewareFull(lookup, nil, "")
}

// NewAuthMiddlewareWithVK creates an auth middleware that also accepts virtual keys.
func NewAuthMiddlewareWithVK(lookup UserLookup, vkLookup VirtualKeyLookup) fiber.Handler {
	return newAuthMiddlewareFull(lookup, vkLookup, "")
}

// NewAuthMiddlewareWithJWT creates an auth middleware that additionally accepts SAML-issued
// JWTs carrying bundle_ids. jwtSecret must match the admin service's JWT_SECRET.
func NewAuthMiddlewareWithJWT(lookup UserLookup, vkLookup VirtualKeyLookup, jwtSecret string) fiber.Handler {
	return newAuthMiddlewareFull(lookup, vkLookup, jwtSecret)
}

func newAuthMiddlewareFull(lookup UserLookup, vkLookup VirtualKeyLookup, jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "missing Authorization header", "type": "auth_error"},
			})
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid Authorization format", "type": "auth_error"},
			})
		}

		// Detect SAML-issued JWTs by attempting JWT parsing when a secret is configured.
		// JWTs contain dots; opaque API keys never do.
		if jwtSecret != "" && strings.Count(token, ".") == 2 {
			if user, ok := tryParseJWT(token, jwtSecret); ok {
				c.Locals("user", user)
				return c.Next()
			}
			// If JWT parsing fails, fall through to API key lookup so a dotted
			// opaque key still works (extremely unlikely but safe).
		}

		keyHash := hashKey(token)

		// Primary user lookup (API keys stored in users table).
		user, err := lookup.LookupByKeyHash(keyHash)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": fiber.Map{"message": "auth lookup failed", "type": "server_error"},
			})
		}

		if user == nil && vkLookup != nil {
			// Fall through to virtual key lookup.
			vk, vkErr := vkLookup.GetByHash(c.Context(), keyHash)
			if vkErr != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": fiber.Map{"message": "auth lookup failed", "type": "server_error"},
				})
			}
			if vk.TenantID != "" {
				// Build a synthetic UserInfo from the virtual key.
				aliases := map[string]string{}
				if vk.ModelAlias != nil && *vk.ModelAlias != "" {
					// A single override alias: any model request is replaced.
					aliases["*"] = *vk.ModelAlias
				}
				user = &UserInfo{
					UserID:       vk.ID,
					TenantID:     vk.TenantID,
					Role:         "user",
					ModelAliases: aliases,
					BundleIDs:    vk.BundleIDs,
					PIIPolicy:    vk.PIIPolicy,
				}
			}
		}

		if user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid API key", "type": "auth_error"},
			})
		}

		c.Locals("user", user)
		return c.Next()
	}
}

// tryParseJWT validates a JWT signed with jwtSecret and converts it to UserInfo.
// Returns (user, true) on success, (nil, false) if the token is invalid.
func tryParseJWT(tokenStr, jwtSecret string) (*UserInfo, bool) {
	parsed, err := jwt.ParseWithClaims(tokenStr, &samlJWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !parsed.Valid {
		return nil, false
	}
	claims, ok := parsed.Claims.(*samlJWTClaims)
	if !ok || claims.TenantID == "" {
		return nil, false
	}
	return &UserInfo{
		UserID:    claims.UserID,
		TenantID:  claims.TenantID,
		Role:      claims.Role,
		BundleIDs: claims.BundleIDs,
	}, true
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
