package ratelimit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	sqlc "github.com/Tarat0r/namepoll/internal/database/sqlc"
)

type Limiter struct {
	db       *sql.DB
	queries  *sqlc.Queries
	cooldown time.Duration
	now      func() time.Time
}

func New(db *sql.DB, cooldown time.Duration) *Limiter {
	return &Limiter{db: db, queries: sqlc.New(db), cooldown: cooldown, now: time.Now}
}

// Allow persists only a hash of the client key, so IP addresses are not stored.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, error) {
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])
	now := l.now().UTC()

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin rate-limit transaction: %w", err)
	}
	defer tx.Rollback()
	queries := l.queries.WithTx(tx)
	if err := queries.PruneRateLimits(ctx, now.Add(-l.cooldown)); err != nil {
		return false, fmt.Errorf("prune rate limits: %w", err)
	}

	rateLimit, err := queries.GetRateLimit(ctx, keyHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("read rate limit: %w", err)
	}
	if err == nil && now.Sub(rateLimit.LastAttempt) < l.cooldown {
		return false, nil
	}

	if err := queries.UpsertRateLimit(ctx, sqlc.UpsertRateLimitParams{
		KeyHash:     keyHash,
		LastAttempt: now,
	}); err != nil {
		return false, fmt.Errorf("write rate limit: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit rate limit: %w", err)
	}
	return true, nil
}

type ClientIPResolver struct {
	trusted []*net.IPNet
}

func NewClientIPResolver(cidrs string) (*ClientIPResolver, error) {
	resolver := &ClientIPResolver{}
	for _, raw := range strings.Split(cidrs, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", raw)
			}
			if ip.To4() != nil {
				raw += "/32"
			} else {
				raw += "/128"
			}
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		resolver.trusted = append(resolver.trusted, network)
	}
	return resolver, nil
}

func (r *ClientIPResolver) ClientIP(request *http.Request) string {
	peer := parseRemoteIP(request.RemoteAddr)
	if peer == nil {
		return request.RemoteAddr
	}
	if !r.isTrusted(peer) {
		return peer.String()
	}

	forwarded := request.Header.Values("X-Forwarded-For")
	var chain []net.IP
	for _, header := range forwarded {
		for _, raw := range strings.Split(header, ",") {
			if ip := net.ParseIP(strings.TrimSpace(raw)); ip != nil {
				chain = append(chain, ip)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if !r.isTrusted(chain[i]) {
			return chain[i].String()
		}
	}
	if len(chain) > 0 {
		return chain[0].String()
	}

	if realIP := net.ParseIP(strings.TrimSpace(request.Header.Get("X-Real-IP"))); realIP != nil {
		return realIP.String()
	}
	return peer.String()
}

func (r *ClientIPResolver) isTrusted(ip net.IP) bool {
	for _, network := range r.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(remoteAddr)
}
