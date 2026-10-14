package ratelimit

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/VictorBontorin/api-gateway/internal/auth"
)

// Janela fixa atômica: INCR + PEXPIRE no primeiro hit.
var script = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
if c == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local ttl = redis.call('PTTL', KEYS[1])
return {c, ttl}
`)

type Limiter struct {
	rdb    *redis.Client
	window time.Duration
	log    *slog.Logger
}

func New(rdb *redis.Client, window time.Duration, log *slog.Logger) *Limiter {
	return &Limiter{rdb: rdb, window: window, log: log}
}

// Middleware limita por identidade (JWT/API key) ou por IP quando anônimo.
func (l *Limiter) Middleware(route string, limit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := auth.Identity(r.Context())
			if id == "" {
				host, _, _ := net.SplitHostPort(r.RemoteAddr)
				id = "ip:" + host
			}
			key := "rl:" + route + ":" + id

			res, err := script.Run(r.Context(), l.rdb, []string{key}, l.window.Milliseconds()).Slice()
			if err != nil || len(res) != 2 {
				l.log.Error("rate limit indisponível, liberando requisição", "err", err)
				next.ServeHTTP(w, r) // fail-open
				return
			}
			count, _ := res[0].(int64)
			ttl, _ := res[1].(int64)

			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

			if count > int64(limit) {
				secs := (ttl + 999) / 1000
				w.Header().Set("Retry-After", strconv.FormatInt(secs, 10))
				auth.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
