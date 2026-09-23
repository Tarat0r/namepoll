package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tarat0r/namepoll/internal/database"
	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
)

func TestLimiterPersistsWithoutStoringClientKey(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "rate-limit.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	first := New(db, time.Minute)
	first.now = func() time.Time { return now }
	allowed, err := first.Allow(context.Background(), "203.0.113.7")
	if err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v; want true, nil", allowed, err)
	}

	second := New(db, time.Minute)
	second.now = func() time.Time { return now.Add(30 * time.Second) }
	allowed, err = second.Allow(context.Background(), "203.0.113.7")
	if err != nil || allowed {
		t.Fatalf("persistent Allow() = %v, %v; want false, nil", allowed, err)
	}

	digest := sha256.Sum256([]byte("203.0.113.7"))
	expectedHash := hex.EncodeToString(digest[:])
	storedLimit, err := sqlc.New(db).GetRateLimit(context.Background(), expectedHash)
	if err != nil {
		t.Fatalf("read stored rate-limit key: %v", err)
	}
	stored := storedLimit.KeyHash
	if stored == "203.0.113.7" || len(stored) != 64 {
		t.Fatalf("stored rate-limit key %q is not a SHA-256 hash", stored)
	}

	second.now = func() time.Time { return now.Add(61 * time.Second) }
	allowed, err = second.Allow(context.Background(), "203.0.113.7")
	if err != nil || !allowed {
		t.Fatalf("expired Allow() = %v, %v; want true, nil", allowed, err)
	}
}

func TestClientIPResolverTrustsOnlyConfiguredProxies(t *testing.T) {
	resolver, err := NewClientIPResolver("10.0.0.0/8, 2001:db8::/32")
	if err != nil {
		t.Fatalf("create resolver: %v", err)
	}

	untrusted := httptest.NewRequest("GET", "/", nil)
	untrusted.RemoteAddr = "198.51.100.9:1234"
	untrusted.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := resolver.ClientIP(untrusted); got != "198.51.100.9" {
		t.Fatalf("untrusted proxy client IP = %q", got)
	}

	trusted := httptest.NewRequest("GET", "/", nil)
	trusted.RemoteAddr = "10.0.0.2:1234"
	trusted.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := resolver.ClientIP(trusted); got != "203.0.113.7" {
		t.Fatalf("trusted proxy client IP = %q", got)
	}

	if _, err := NewClientIPResolver("not-a-network"); err == nil {
		t.Fatal("invalid trusted proxy configuration was accepted")
	}
}
