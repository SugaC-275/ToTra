package providers

import (
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

func TestSizeToAspectRatio(t *testing.T) {
	cases := []struct {
		size string
		want string
	}{
		{"1024x1024", "1:1"},
		{"", "1:1"},
		{"1792x1024", "7:4"},
		{"1024x1792", "4:7"},
		{"1280x720", "16:9"},
		{"720x1280", "9:16"},
		{"512x512", "1:1"}, // unknown → default 1:1
	}
	for _, tc := range cases {
		got := sizeToAspectRatio(tc.size)
		if got != tc.want {
			t.Errorf("sizeToAspectRatio(%q) = %q, want %q", tc.size, got, tc.want)
		}
	}
}

func TestBuildStabilityForm_Fields(t *testing.T) {
	req := stabilityRequest{
		Prompt:         "a cat on a surfboard",
		NegativePrompt: "blurry",
		Size:           "1792x1024",
		OutputFormat:   "jpeg",
	}

	buf, contentType, err := buildStabilityForm(req)
	if err != nil {
		t.Fatalf("buildStabilityForm: %v", err)
	}

	// Parse boundary from Content-Type header.
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse media type: %v", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("no boundary in Content-Type")
	}

	mr := multipart.NewReader(strings.NewReader(buf.String()), boundary)
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		data, _ := io.ReadAll(part)
		fields[part.FormName()] = string(data)
	}

	if fields["prompt"] != "a cat on a surfboard" {
		t.Errorf("prompt = %q", fields["prompt"])
	}
	if fields["negative_prompt"] != "blurry" {
		t.Errorf("negative_prompt = %q", fields["negative_prompt"])
	}
	if fields["aspect_ratio"] != "7:4" {
		t.Errorf("aspect_ratio = %q, want 7:4", fields["aspect_ratio"])
	}
	if fields["output_format"] != "jpeg" {
		t.Errorf("output_format = %q, want jpeg", fields["output_format"])
	}
}

func TestBuildStabilityForm_Defaults(t *testing.T) {
	req := stabilityRequest{Prompt: "sunset"}
	buf, contentType, err := buildStabilityForm(req)
	if err != nil {
		t.Fatalf("buildStabilityForm: %v", err)
	}

	_, params, _ := mime.ParseMediaType(contentType)
	mr := multipart.NewReader(strings.NewReader(buf.String()), params["boundary"])
	fields := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		data, _ := io.ReadAll(part)
		fields[part.FormName()] = string(data)
	}

	if fields["output_format"] != "png" {
		t.Errorf("default output_format = %q, want png", fields["output_format"])
	}
	if fields["aspect_ratio"] != "1:1" {
		t.Errorf("default aspect_ratio = %q, want 1:1", fields["aspect_ratio"])
	}
	// negative_prompt should be absent when empty.
	if _, ok := fields["negative_prompt"]; ok {
		t.Error("negative_prompt should not be present when empty")
	}
}

func TestStabilityForwardStream_NotSupported(t *testing.T) {
	a := NewStabilityAdapter("https://api.stability.ai", "key")
	err := a.ForwardStream(nil, nil, nil) //nolint:staticcheck
	if err != ErrNotSupported {
		t.Errorf("ForwardStream = %v, want ErrNotSupported", err)
	}
}

func TestStabilityAdapter_RegisteredInRegistry(t *testing.T) {
	adapter, err := New("stability", "https://api.stability.ai", "testkey")
	if err != nil {
		t.Fatalf("New(stability): %v", err)
	}
	if _, ok := adapter.(*StabilityAdapter); !ok {
		t.Errorf("expected *StabilityAdapter, got %T", adapter)
	}
}
