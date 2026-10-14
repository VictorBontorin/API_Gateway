package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Route struct {
	Prefix      string `json:"prefix"`
	Upstream    string `json:"upstream"`
	StripPrefix bool   `json:"strip_prefix"`
	Auth        *bool  `json:"auth"`       // padrão: true
	RateLimit   int    `json:"rate_limit"` // 0 = usa o padrão global
}

func (r Route) RequiresAuth() bool { return r.Auth == nil || *r.Auth }

type Config struct {
	Env        string
	Addr       string
	JWTSecret  string
	RedisAddr  string
	RateLimit  int
	RateWindow time.Duration
	APIKeys    map[string]string // chave -> nome
	Routes     []Route
}

func Load() (*Config, error) {
	c := &Config{
		Env:       get("APP_ENV", "dev"),
		Addr:      get("GATEWAY_ADDR", ":8080"),
		JWTSecret: os.Getenv("JWT_SECRET"),
		RedisAddr: get("REDIS_ADDR", "localhost:6379"),
		APIKeys:   map[string]string{},
	}
	if c.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET é obrigatório")
	}
	var err error
	if c.RateLimit, err = strconv.Atoi(get("RATE_LIMIT_DEFAULT", "60")); err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_DEFAULT inválido: %w", err)
	}
	if c.RateWindow, err = time.ParseDuration(get("RATE_LIMIT_WINDOW", "1m")); err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_WINDOW inválido: %w", err)
	}
	for _, kv := range strings.Split(os.Getenv("API_KEYS"), ",") {
		if name, key, ok := strings.Cut(strings.TrimSpace(kv), ":"); ok && key != "" {
			c.APIKeys[key] = name
		}
	}
	raw, err := os.ReadFile(get("ROUTES_FILE", "routes.json"))
	if err != nil {
		return nil, fmt.Errorf("lendo rotas: %w", err)
	}
	if err := json.Unmarshal(raw, &c.Routes); err != nil {
		return nil, fmt.Errorf("parse das rotas: %w", err)
	}
	return c, nil
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
