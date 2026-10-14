package gateway_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"

	"github.com/VictorBontorin/api-gateway/internal/config"
	"github.com/VictorBontorin/api-gateway/internal/gateway"
)

const secret = "test-secret"

func setup(t *testing.T, limit int) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.URL.Path+"|"+r.Header.Get("X-User-ID"))
	}))
	t.Cleanup(upstream.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	cfg := &config.Config{
		JWTSecret:  secret,
		RateLimit:  limit,
		RateWindow: time.Minute,
		APIKeys:    map[string]string{"k1": "partner"},
		Routes:     []config.Route{{Prefix: "/svc", Upstream: upstream.URL, StripPrefix: true}},
	}
	h, err := gateway.New(cfg, rdb, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(h)
	t.Cleanup(gw.Close)
	return gw
}

func token(t *testing.T, sub string, exp time.Duration) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": sub, "exp": time.Now().Add(exp).Unix(),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func do(t *testing.T, url string, headers map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestUnauthorized(t *testing.T) {
	gw := setup(t, 10)
	if code, _ := do(t, gw.URL+"/svc/x", nil); code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", code)
	}
	if code, _ := do(t, gw.URL+"/svc/x", map[string]string{"Authorization": "Bearer " + token(t, "u1", -time.Hour)}); code != http.StatusUnauthorized {
		t.Fatalf("expired token: want 401, got %d", code)
	}
}

func TestProxyWithJWT(t *testing.T) {
	gw := setup(t, 10)
	code, body := do(t, gw.URL+"/svc/orders", map[string]string{
		"Authorization": "Bearer " + token(t, "u1", time.Hour),
		"X-User-ID":     "spoofed",
	})
	if code != http.StatusOK || body != "/orders|user:u1" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestAPIKey(t *testing.T) {
	gw := setup(t, 10)
	code, body := do(t, gw.URL+"/svc/a", map[string]string{"X-API-Key": "k1"})
	if code != http.StatusOK || body != "/a|key:partner" {
		t.Fatalf("got %d %q", code, body)
	}
}

func TestRateLimit(t *testing.T) {
	gw := setup(t, 2)
	h := map[string]string{"Authorization": "Bearer " + token(t, "u1", time.Hour)}
	for i := 0; i < 2; i++ {
		if code, _ := do(t, gw.URL+"/svc/x", h); code != http.StatusOK {
			t.Fatalf("req %d: got %d", i, code)
		}
	}
	if code, _ := do(t, gw.URL+"/svc/x", h); code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", code)
	}
	// outro usuário não é afetado
	if code, _ := do(t, gw.URL+"/svc/x", map[string]string{"Authorization": "Bearer " + token(t, "u2", time.Hour)}); code != http.StatusOK {
		t.Fatalf("u2: got %d", code)
	}
}
