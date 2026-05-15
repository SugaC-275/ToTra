package storage_test

import (
	"testing"

	"github.com/yourorg/totra/gateway/storage"
)

func TestSIEMGatewayStore_NewReturnsNonNil(t *testing.T) {
	s := storage.NewSIEMGatewayStore(nil)
	if s == nil {
		t.Fatal("expected non-nil SIEMGatewayStore")
	}
}
