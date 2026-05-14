package storage

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ViolationRecord struct {
	TenantID    string
	UserID      string
	PIIType     string
	Action      string
	RequestPath string
}

func BuildViolationRecord(tenantID, userID, piiType, action, requestPath string) ViolationRecord {
	return ViolationRecord{TenantID: tenantID, UserID: userID, PIIType: piiType, Action: action, RequestPath: requestPath}
}

type PIIStore struct {
	ch chan ViolationRecord
	db *pgxpool.Pool
}

func NewPIIStore(db *pgxpool.Pool, bufSize int) *PIIStore {
	s := &PIIStore{ch: make(chan ViolationRecord, bufSize), db: db}
	go s.flush()
	return s
}

func (s *PIIStore) Record(r ViolationRecord) {
	select {
	case s.ch <- r:
	default:
		log.Println("pii_store: buffer full, dropping violation record")
	}
}

func (s *PIIStore) flush() {
	for r := range s.ch {
		var userID *string
		if r.UserID != "" {
			userID = &r.UserID
		}
		_, err := s.db.Exec(context.Background(),
			`INSERT INTO pii_violations (tenant_id, user_id, pii_type, action, request_path) VALUES ($1,$2,$3,$4,$5)`,
			r.TenantID, userID, r.PIIType, r.Action, r.RequestPath)
		if err != nil {
			log.Printf("pii_store: insert error: %v", err)
		}
	}
}
