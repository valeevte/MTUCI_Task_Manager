package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
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

	t.Run("query is deliberately ignored", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tasks?initData=query-token", nil)
		if got := extractInitData(req); got != "" {
			t.Fatalf("expected an empty token, got %q", got)
		}
	})

	t.Run("unsupported authorization scheme", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tasks", nil)
		req.Header.Set("Authorization", "Bearer abc123")
		if got := extractInitData(req); got != "" {
			t.Fatalf("expected an empty token, got %q", got)
		}
	})
}

func signedInitData(t *testing.T, token string, authDate time.Time) string {
	t.Helper()

	values := url.Values{
		"auth_date": {strconv.FormatInt(authDate.Unix(), 10)},
		"query_id":  {"query"},
		"user":      {`{"id":42,"first_name":"Test","username":"test_user"}`},
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+values.Get(key))
	}

	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(token))
	hashMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = hashMAC.Write([]byte(strings.Join(pairs, "\n")))
	values.Set("hash", hex.EncodeToString(hashMAC.Sum(nil)))
	return values.Encode()
}

func TestValidateInitData(t *testing.T) {
	const token = "123:token"
	now := time.Unix(1_800_000_000, 0)

	t.Run("accepts a fresh signed payload", func(t *testing.T) {
		user, err := validateInitDataAt(signedInitData(t, token, now.Add(-time.Hour)), token, now)
		if err != nil {
			t.Fatalf("validateInitDataAt error: %v", err)
		}
		if user.ID != 42 {
			t.Fatalf("expected user id 42, got %d", user.ID)
		}
	})

	t.Run("rejects an expired payload", func(t *testing.T) {
		_, err := validateInitDataAt(signedInitData(t, token, now.Add(-initDataMaxAge-time.Second)), token, now)
		if err == nil {
			t.Fatal("expected expired initData to be rejected")
		}
	})

	t.Run("rejects a future payload", func(t *testing.T) {
		_, err := validateInitDataAt(signedInitData(t, token, now.Add(maxClockSkew+time.Second)), token, now)
		if err == nil {
			t.Fatal("expected future initData to be rejected")
		}
	})
}
