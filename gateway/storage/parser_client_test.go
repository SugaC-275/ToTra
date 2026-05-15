package storage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func TestParserClient_Parse_PDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/parse", r.URL.Path)
		r.ParseMultipartForm(1 << 20)
		_, header, err := r.FormFile("file")
		require.NoError(t, err)
		assert.Equal(t, "test.pdf", header.Filename)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"extracted content","page_count":3}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	result, err := client.Parse(context.Background(), "test.pdf", []byte("fake pdf bytes"))
	require.NoError(t, err)
	assert.Equal(t, "extracted content", result.Text)
	assert.Equal(t, 3, result.PageCount)
}

func TestParserClient_Parse_UnsupportedFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"unsupported format"}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	_, err := client.Parse(context.Background(), "test.xls", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestParserClient_Parse_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	_, err := client.Parse(context.Background(), "test.pdf", []byte("data"))
	require.Error(t, err)
}
