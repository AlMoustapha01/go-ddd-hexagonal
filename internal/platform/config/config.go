// Package config loads application configuration from the environment.
// No external dependency: it reads os.Getenv with sane defaults so the
// boilerplate runs out of the box.
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	// HTTPAddr is the listen address, e.g. ":8080".
	HTTPAddr string
	// LogLevel: debug|info|warn|error.
	LogLevel string
	// LogFormat: json|text.
	LogFormat string
	// Driver selects the repository adapter: "memory" or "postgres".
	Driver string
	// DatabaseURL is required when Driver == "postgres".
	DatabaseURL string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8080"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "json"),
		Driver:      strings.ToLower(getEnv("DB_DRIVER", "memory")),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}

	switch cfg.Driver {
	case "memory":
	case "postgres":
		if cfg.DatabaseURL == "" {
			return Config{}, fmt.Errorf("config: DATABASE_URL is required when DB_DRIVER=postgres")
		}
	default:
		return Config{}, fmt.Errorf("config: unknown DB_DRIVER %q (want memory|postgres)", cfg.Driver)
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
