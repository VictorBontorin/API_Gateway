package observability

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

)

func NewLogger(env string) *slog.Logger {
	if env == "prod" {
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type Metrics struct {
	reg  *prometheus.Registry
	reqs *prometheus.CounterVec
	dur  *prometheus.HistogramVec
}

func NewMetrics() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		reqs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gateway_requests_total", Help: "Total de requisições.",
		}, []string{"route", "method", "status"}),
		dur: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "gateway_request_duration_seconds", Help: "Latência das requisições.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
	}
	m.reg.MustRegister(m.reqs, m.dur, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Observe é o middleware mais externo de cada rota: request id, log e métricas.
func Observe(route string, log *slog.Logger, m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID := r.Header.Get("X-Request-ID")
			if reqID == "" {
				b := make([]byte, 8)
				_, _ = rand.Read(b)
				reqID = hex.EncodeToString(b)
				r.Header.Set("X-Request-ID", reqID)
			}
			w.Header().Set("X-Request-ID", reqID)

			rec := &recorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			elapsed := time.Since(start)
			m.reqs.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
			m.dur.WithLabelValues(route).Observe(elapsed.Seconds())
			log.Info("request",
				"request_id", reqID,
				"route", route,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}
