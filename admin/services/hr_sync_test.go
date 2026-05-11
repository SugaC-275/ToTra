package services_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/services"
)

func TestParseHRCSV_Valid(t *testing.T) {
	csv := "email,name,role,department\nalice@acme.com,Alice,employee,Engineering\nbob@acme.com,Bob,employee,Product\n"
	records, err := services.ParseHRCSV(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, records, 2)
	assert.Equal(t, "alice@acme.com", records[0].Email)
	assert.Equal(t, "Alice", records[0].Name)
	assert.Equal(t, "employee", records[0].Role)
	assert.Equal(t, "Engineering", records[0].Department)
}

func TestParseHRCSV_MissingHeader(t *testing.T) {
	csv := "alice@acme.com,Alice,employee,Engineering\n"
	_, err := services.ParseHRCSV(strings.NewReader(csv))
	assert.Error(t, err)
}

func TestParseHRCSV_EmptyEmail(t *testing.T) {
	csv := "email,name,role,department\n,Alice,employee,Engineering\n"
	_, err := services.ParseHRCSV(strings.NewReader(csv))
	assert.Error(t, err)
}

func TestParseHRCSV_ExtraColumns(t *testing.T) {
	csv := "email,name,role,department,extra\nalice@acme.com,Alice,employee,Engineering,ignored\n"
	records, err := services.ParseHRCSV(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Len(t, records, 1)
	assert.Equal(t, "Engineering", records[0].Department)
}

func TestParseHRCSV_InvalidRole(t *testing.T) {
	csv := "email,name,role,department\nalice@acme.com,Alice,superadmin,Engineering\n"
	_, err := services.ParseHRCSV(strings.NewReader(csv))
	assert.Error(t, err)
}
