package api

import (
	"net/http/httptest"
	"testing"
)

func TestExtractInitData(t *testing.T) {
	t.Run("authorization tma", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tasks", nil)
		req.Header.Set("Authorization", "tma abc123")
		if got := extractInitData(req); got != "abc123" {
			t.Fatalf("expected abc123, got %q", got)
		}
	})

	t.Run("fallback header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tasks", nil)
		req.Header.Set("X-Telegram-Init-Data", "header-token")
		if got := extractInitData(req); got != "header-token" {
			t.Fatalf("expected header-token, got %q", got)
		}
	})

	t.Run("query fallback", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tasks?initData=query-token", nil)
		if got := extractInitData(req); got != "query-token" {
			t.Fatalf("expected query-token, got %q", got)
		}
	})
}
