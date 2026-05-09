package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	QuotaSCU int    `json:"quota_scu"`
	IsActive bool   `json:"is_active"`
	APIKey   string `json:"api_key,omitempty"`
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

type UserServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*User, error)
	Create(ctx context.Context, tenantID string, req CreateUserRequest) (*User, error)
}

type UserService struct {
	pool *pgxpool.Pool
}

func NewUserService(pool *pgxpool.Pool) *UserService {
	return &UserService{pool: pool}
}

func (s *UserService) List(ctx context.Context, tenantID string) ([]*User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, email, role, quota_scu, is_active FROM users WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Name, &u.Email, &u.Role, &u.QuotaSCU, &u.IsActive); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *UserService) Create(ctx context.Context, tenantID string, req CreateUserRequest) (*User, error) {
	rawKey := generateEmployeeKey()
	keyHash := hashEmployeeKey(rawKey)

	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, role)
		 VALUES ($1, $2, $3, $4, 'api_key', $5, $6) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Email, keyHash, req.Role,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return &User{ID: id, TenantID: tenantID, Name: req.Name, Email: req.Email, Role: req.Role, APIKey: rawKey}, nil
}

func generateEmployeeKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "totra-emp-" + hex.EncodeToString(b)
}

func hashEmployeeKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
