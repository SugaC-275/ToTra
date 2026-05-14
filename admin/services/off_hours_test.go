package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestIsWorkHour_During(t *testing.T) {
	assert.True(t, services.IsWorkHour(9, 9, 18))
	assert.True(t, services.IsWorkHour(17, 9, 18))
}

func TestIsWorkHour_Outside(t *testing.T) {
	assert.False(t, services.IsWorkHour(8, 9, 18))
	assert.False(t, services.IsWorkHour(18, 9, 18))
	assert.False(t, services.IsWorkHour(23, 9, 18))
}
