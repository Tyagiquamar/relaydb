package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tyagiquamar/relaydb/internal/config"
)

func TestAuthParsing(t *testing.T) {
	s := &Server{}

	tests := []struct {
		name     string
		header   string
		wantID   string
		wantKey  string
	}{
		{"valid", "Bearer admin:secret123", "admin", "secret123"},
		{"no bearer", "secret123", "", ""},
		{"empty", "", "", ""},
		{"no colon", "Bearer secret123", "", "secret123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Authorization", tt.header)

			id, key := s.parseAuth(req)
			if id != tt.wantID || key != tt.wantKey {
				t.Errorf("parseAuth() = (%q, %q), want (%q, %q)", id, key, tt.wantID, tt.wantKey)
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	s := &Server{}

	rec := httptest.NewRecorder()
	s.writeJSON(rec, 200, map[string]string{"status": "ok"})

	if rec.Code != 200 {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("status = %q, want ok", result["status"])
	}
}

func TestWriteError(t *testing.T) {
	s := &Server{}

	rec := httptest.NewRecorder()
	s.writeError(rec, 400, "bad request")

	if rec.Code != 400 {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["message"] != "bad request" {
		t.Errorf("message = %q, want %q", result["message"], "bad request")
	}
}

func TestServerCreation(t *testing.T) {
	cfg := config.Config{
		AdminAPIKeyID:  "admin",
		AdminAPIKey:    "admin-secret",
		ReaderAPIKeyID: "reader",
		ReaderAPIKey:   "reader-secret",
	}

	s := NewServer(cfg, nil)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}

	handler := s.Handler()
	if handler == nil {
		t.Fatal("Handler returned nil")
	}
}