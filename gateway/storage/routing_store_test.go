package storage_test

import (
	"testing"

	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

func TestRoutingStore_ImplementsInterface(t *testing.T) {
	var _ middleware.RoutingRecorder = (*storage.RoutingStore)(nil)
}
