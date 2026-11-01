package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOIDCLoginStartLimiterBoundsClientsAndGlobalCapacity(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	limiter := newOIDCLoginStartLimiter()
	limiter.now = func() time.Time { return now }

	for range oidcLoginStartClientLimit {
		if allowed, _ := limiter.allow("192.0.2.1"); !allowed {
			t.Fatal("client was limited before reaching its allowance")
		}
	}
	if allowed, retryAfter := limiter.allow("192.0.2.1"); allowed || retryAfter != time.Minute {
		t.Fatalf("client limit = (%t, %s), want (false, 1m)", allowed, retryAfter)
	}

	for index := oidcLoginStartClientLimit; index < oidcLoginStartGlobalLimit; index++ {
		client := fmt.Sprintf("198.51.100.%d", index)
		if allowed, _ := limiter.allow(client); !allowed {
			t.Fatalf("global request %d was limited before reaching capacity", index)
		}
	}
	if allowed, retryAfter := limiter.allow("203.0.113.1"); allowed || retryAfter != 10*time.Minute {
		t.Fatalf("global limit = (%t, %s), want (false, 10m)", allowed, retryAfter)
	}

	now = now.Add(10 * time.Minute)
	if allowed, _ := limiter.allow("192.0.2.1"); !allowed {
		t.Fatal("expired attempts still consume capacity")
	}
}

func TestOIDCLoginStartLimitUsesConfiguredProxyHops(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/login", nil)
	request.RemoteAddr = "203.0.113.10:443"
	request.Header.Set("X-Forwarded-For", "192.0.2.10, 198.51.100.10")

	if address := requestClientAddress(request, 0); address != "203.0.113.10" {
		t.Errorf("direct address = %q, want remote peer", address)
	}
	if address := requestClientAddress(request, 1); address != "198.51.100.10" {
		t.Errorf("one-hop address = %q, want nearest forwarded address", address)
	}
	if address := requestClientAddress(request, 2); address != "192.0.2.10" {
		t.Errorf("two-hop address = %q, want original client", address)
	}
}

func TestOIDCLoginStartLimitReturnsRetryAfter(t *testing.T) {
	t.Parallel()

	limiter := newOIDCLoginStartLimiter()
	limiter.now = func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) }
	called := 0
	handler := limitOIDCLoginStarts(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		called++
		writer.WriteHeader(http.StatusNoContent)
	}), limiter, 0)

	for range oidcLoginStartClientLimit + 1 {
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/login", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if called <= oidcLoginStartClientLimit && response.Code == http.StatusTooManyRequests {
			if response.Header().Get("Retry-After") != "60" {
				t.Errorf("Retry-After = %q, want 60", response.Header().Get("Retry-After"))
			}
		}
	}

	if called != oidcLoginStartClientLimit {
		t.Errorf("allowed requests = %d, want %d", called, oidcLoginStartClientLimit)
	}
}
