package services_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

func TestComputeRecordHash_Deterministic(t *testing.T) {
	payload := map[string]any{"user_id": "u1", "score": 0.85}
	h1, err := services.ComputeRecordHash(payload)
	require.NoError(t, err)
	h2, err := services.ComputeRecordHash(payload)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64)
}

func TestComputeRecordHash_MatchesManual(t *testing.T) {
	payload := map[string]any{"x": 1}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	expected := hex.EncodeToString(sum[:])

	got, err := services.ComputeRecordHash(payload)
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestComputeChainHash_Genesis(t *testing.T) {
	recordHash := "aabbcc"
	chain := services.ComputeChainHash("genesis", recordHash)
	assert.Len(t, chain, 64)

	sum := sha256.Sum256([]byte("genesis" + recordHash))
	assert.Equal(t, hex.EncodeToString(sum[:]), chain)
}

func TestComputeChainHash_Chaining(t *testing.T) {
	rh1 := "aaaa"
	rh2 := "bbbb"
	c1 := services.ComputeChainHash("genesis", rh1)
	c2 := services.ComputeChainHash(c1, rh2)
	assert.NotEqual(t, c1, c2)
}

func TestComputeChainHash_TamperDetection(t *testing.T) {
	c1 := services.ComputeChainHash("genesis", "legit_hash")
	c2 := services.ComputeChainHash("genesis", "tampered_hash")
	assert.NotEqual(t, c1, c2)
}
