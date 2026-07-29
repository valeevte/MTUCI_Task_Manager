package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mtuci-task-manager/bot"
)

func callCreateTask(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()

	srv := NewServer(bot.NewStorage(), "token")
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", bytes.NewBufferString(body))
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, &TelegramUser{
		ID:        1,
		FirstName: "Test",
	}))
	rr := httptest.NewRecorder()
	srv.handleCreateTask(rr, req)
	return rr
}

func TestCreateTaskRejectsAmbiguousJSON(t *testing.T) {
	t.Run("unknown field", func(t *testing.T) {
		rr := callCreateTask(t, `{"title":"Task","titel":"typo"}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("second object", func(t *testing.T) {
		rr := callCreateTask(t, `{"title":"First"}{"title":"Second"}`)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("body limit", func(t *testing.T) {
		body := `{"title":"Task","description":"` + strings.Repeat("x", maxJSONBodySize) + `"}`
		rr := callCreateTask(t, body)
		if rr.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestPositiveID(t *testing.T) {
	for _, value := range []string{"", "abc", "0", "-1"} {
		if _, err := positiveID(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
	if id, err := positiveID("42"); err != nil || id != 42 {
		t.Fatalf("expected id 42, got id=%d err=%v", id, err)
	}
}
