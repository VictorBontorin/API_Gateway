package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey struct{}

// Identity devolve a identidade autenticada ("user:<sub>" ou "key:<nome>").
func Identity(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func Middleware(secret []byte, apiKeys map[string]string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := authenticate(r, secret, apiKeys)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="gateway"`)
				WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
		})
	}
}

func authenticate(r *http.Request, secret []byte, apiKeys map[string]string) (string, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok, err := jwt.Parse(strings.TrimPrefix(h, "Bearer "),
			func(*jwt.Token) (any, error) { return secret, nil },
			jwt.WithValidMethods([]string{"HS256"}),
			jwt.WithExpirationRequired(),
		)
		if err != nil || !tok.Valid {
			return "", false
		}
		sub, err := tok.Claims.GetSubject()
		if err != nil || sub == "" {
			return "", false
		}
		return "user:" + sub, true
	}
	if key := r.Header.Get("X-API-Key"); key != "" {
		if name, ok := apiKeys[key]; ok {
			return "key:" + name, true
		}
	}
	return "", false
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
