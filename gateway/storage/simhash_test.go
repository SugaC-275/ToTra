package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/storage"
)

func TestSimHash64_EmptyString(t *testing.T) {
	assert.Equal(t, uint64(0), storage.SimHash64(""))
}

func TestSimHash64_Deterministic(t *testing.T) {
	h1 := storage.SimHash64("how do I sort a list in Go?")
	h2 := storage.SimHash64("how do I sort a list in Go?")
	assert.Equal(t, h1, h2)
}

func TestSimHash64_SimilarTextsSmallDistance(t *testing.T) {
	a := storage.SimHash64("how do I sort a list in Go?")
	b := storage.SimHash64("how can I sort a list in Go?")
	dist := storage.HammingDistance(a, b)
	assert.LessOrEqual(t, dist, 10, "paraphrased query should have small Hamming distance")
}

func TestSimHash64_DifferentTextsLargeDistance(t *testing.T) {
	a := storage.SimHash64("sort a list in Go")
	b := storage.SimHash64("explain quantum entanglement in simple terms")
	dist := storage.HammingDistance(a, b)
	assert.Greater(t, dist, 5, "unrelated texts should have larger Hamming distance")
}

func TestHammingDistance_Identity(t *testing.T) {
	h := storage.SimHash64("hello world")
	assert.Equal(t, 0, storage.HammingDistance(h, h))
}

func TestHammingDistance_Complement(t *testing.T) {
	assert.Equal(t, 64, storage.HammingDistance(0, ^uint64(0)))
}

func TestSimHash64_OrderInsensitive(t *testing.T) {
	a := storage.SimHash64("Go sort list")
	b := storage.SimHash64("list sort Go")
	dist := storage.HammingDistance(a, b)
	assert.Equal(t, 0, dist, "same tokens in different order should produce same hash")
}
