package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestRetentionCutoff_24Months(t *testing.T) {
	cutoff := services.RetentionCutoff(24)
	assert.True(t, cutoff.Before(time.Now()))
	expected := time.Now().AddDate(-2, 0, 0)
	diff := cutoff.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff.Hours(), 25.0)
}

func TestRetentionCutoff_0Months(t *testing.T) {
	cutoff := services.RetentionCutoff(0)
	assert.WithinDuration(t, time.Now(), cutoff, 5*time.Second)
}

func TestRetentionCutoff_1Month(t *testing.T) {
	cutoff := services.RetentionCutoff(1)
	expected := time.Now().AddDate(0, -1, 0)
	diff := cutoff.Sub(expected)
	if diff < 0 {
		diff = -diff
	}
	assert.Less(t, diff.Hours(), 25.0)
}
