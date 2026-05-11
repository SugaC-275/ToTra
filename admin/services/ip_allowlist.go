package services

import (
	"context"
	"fmt"
	"net"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	pgconn "github.com/jackc/pgx/v5/pgconn"
)

// dbQuerier is a minimal interface satisfied by both *pgxpool.Pool and pgxmock.PgxPoolIface.
type dbQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// IPAllowlistEntry represents a single CIDR allowlist entry for a tenant.
type IPAllowlistEntry struct {
	ID        string `json:"id"`
	CIDR      string `json:"cidr"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at"`
}

// IPAllowlistService manages per-tenant IP allowlist entries.
type IPAllowlistService struct {
	db dbQuerier
}

// NewIPAllowlistService creates a new IPAllowlistService.
// Accepts *pgxpool.Pool or any compatible dbQuerier (e.g. pgxmock.PgxPoolIface).
func NewIPAllowlistService(db dbQuerier) *IPAllowlistService {
	return &IPAllowlistService{db: db}
}

// List returns all allowlist entries for the given tenant, ordered by creation time.
func (s *IPAllowlistService) List(ctx context.Context, tenantID string) ([]*IPAllowlistEntry, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, cidr, label, created_at
		FROM tenant_ip_allowlists
		WHERE tenant_id = $1
		ORDER BY created_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*IPAllowlistEntry
	for rows.Next() {
		e := &IPAllowlistEntry{}
		if err := rows.Scan(&e.ID, &e.CIDR, &e.Label, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Add validates the CIDR, inserts a new allowlist entry, and returns it.
func (s *IPAllowlistService) Add(ctx context.Context, tenantID, cidr, label string) (*IPAllowlistEntry, error) {
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	e := &IPAllowlistEntry{}
	err := s.db.QueryRow(ctx, `
		INSERT INTO tenant_ip_allowlists (tenant_id, cidr, label)
		VALUES ($1, $2, $3)
		RETURNING id, cidr, label, created_at`,
		tenantID, cidr, label,
	).Scan(&e.ID, &e.CIDR, &e.Label, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Delete removes an allowlist entry by ID, scoped to the tenant.
func (s *IPAllowlistService) Delete(ctx context.Context, tenantID, id string) error {
	_, err := s.db.Exec(ctx, `
		DELETE FROM tenant_ip_allowlists
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	return err
}

// LoadTenantCIDRs returns all CIDR strings for the given tenant (used by middleware).
func (s *IPAllowlistService) LoadTenantCIDRs(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `
		SELECT cidr FROM tenant_ip_allowlists
		WHERE tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cidrs []string
	for rows.Next() {
		var cidr string
		if err := rows.Scan(&cidr); err != nil {
			return nil, err
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs, rows.Err()
}

// IPAllowlistMiddleware enforces per-tenant IP allowlists for Fiber routes.
// If no entries exist for the tenant, all IPs are allowed (opt-in enforcement).
func IPAllowlistMiddleware(svc *IPAllowlistService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if !ok || claims == nil {
			return c.Next()
		}

		cidrs, err := svc.LoadTenantCIDRs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "allowlist check failed"})
		}

		if len(cidrs) == 0 {
			return c.Next()
		}

		clientIP := net.ParseIP(c.IP())
		if clientIP == nil {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden: unresolvable client IP"})
		}

		for _, cidrStr := range cidrs {
			_, network, err := net.ParseCIDR(cidrStr)
			if err != nil {
				continue
			}
			if network.Contains(clientIP) {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{"error": "forbidden: IP not in allowlist"})
	}
}
