package gateway

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/VictorBontorin/api-gateway/internal/auth"
	"github.com/VictorBontorin/api-gateway/internal/config"
	"github.com/VictorBontorin/api-gateway/internal/observability"
	"github.com/VictorBontorin/api-gateway/internal/ratelimit"
)

func New(cfg *config.Config, rdb *redis.Client, log *slog.Logger) (http.Handler, error) {
	mux := http.NewServeMux()
	metrics := observability.NewMetrics()
	limiter := ratelimit.New(rdb, cfg.RateWindow, log)
	authMW := auth.Middleware([]byte(cfg.JWTSecret), cfg.APIKeys)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("GET /metrics", metrics.Handler())

	for _, rt := range cfg.Routes {
		target, err := url.Parse(rt.Upstream)
		if err != nil {
			return nil, err
		}
		limit := rt.RateLimit
		if limit <= 0 {
			limit = cfg.RateLimit
		}

		var h http.Handler = newProxy(rt, target, log)
		h = limiter.Middleware(rt.Prefix, limit)(h)
		if rt.RequiresAuth() {
			h = authMW(h)
		}
		h = observability.Observe(rt.Prefix, log, metrics)(h)

		mux.Handle(rt.Prefix+"/", h)
	}
	return mux, nil
}

func newProxy(rt config.Route, target *url.URL, log *slog.Logger) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			if rt.StripPrefix {
				p := strings.TrimPrefix(pr.In.URL.Path, rt.Prefix)
				if p == "" {
					p = "/"
				}
				pr.Out.URL.Path = strings.TrimSuffix(target.Path, "/") + p
				pr.Out.URL.RawPath = ""
			}
			// nunca confiar em identidade vinda do cliente
			pr.Out.Header.Del("X-User-ID")
			if id := auth.Identity(pr.In.Context()); id != "" {
				pr.Out.Header.Set("X-User-ID", id)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("upstream error", "route", rt.Prefix, "err", err)
			auth.WriteError(w, http.StatusBadGateway, "bad gateway")
		},
	}
}
