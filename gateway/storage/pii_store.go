package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

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

// RecordViolation implements the middleware.ViolationRecorder interface.
func (s *PIIStore) RecordViolation(tenantID, userID, piiType, action, requestPath string) {
	s.Record(BuildViolationRecord(tenantID, userID, piiType, action, requestPath))
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
			continue
		}
		// Fire-and-forget HTTP alert to admin service
		if adminURL := os.Getenv("ADMIN_INTERNAL_URL"); adminURL != "" {
			go func(rec ViolationRecord) {
				body, _ := json.Marshal(map[string]string{
					"tenant_id":    rec.TenantID,
					"user_id":      rec.UserID,
					"pii_type":     rec.PIIType,
					"request_path": rec.RequestPath,
				})
				req, err := http.NewRequest("POST", adminURL+"/internal/compliance/pii-alert", bytes.NewReader(body))
				if err != nil {
					log.Printf("pii_store: build alert request error: %v", err)
					return
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Internal-Secret", os.Getenv("INTERNAL_SECRET"))
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					log.Printf("pii_store: send alert error: %v", err)
					return
				}
				resp.Body.Close()
			}(r)
		}
	}
}
