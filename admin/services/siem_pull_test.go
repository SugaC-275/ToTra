package services_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestSIEMPullService_NewReturnsNonNil(t *testing.T) {
	svc := services.NewSIEMPullService(nil)
	assert.NotNil(t, svc)
}

func TestSIEMEvent_JSONFields(t *testing.T) {
	detail := json.RawMessage(`{"pii_type":"china_phone"}`)
	ev := services.SIEMEvent{
		ID:         "42",
		Type:       "pii_violation",
		TenantID:   "acme",
		OccurredAt: time.Now(),
		Detail:     detail,
	}
	b, err := json.Marshal(ev)
	assert.NoError(t, err)
	assert.Contains(t, string(b), `"pii_violation"`)
	assert.Contains(t, string(b), `"china_phone"`)
}

func TestSIEMEventsResult_NextSince(t *testing.T) {
	t0 := time.Now()
	res := services.SIEMEventsResult{
		Events:    []*services.SIEMEvent{},
		NextSince: t0,
	}
	assert.Equal(t, t0, res.NextSince)
}
