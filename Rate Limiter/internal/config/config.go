package config

import (
	"os"
	"strconv"
	"strings"

	"rate-limiter/pkg/logging"
)

type ServerConfig struct {
	Port         int      `json:"port"`
	DefaultLimit int      `json:"default_limit"`
	Algorithms   []string `json:"algorithms"`
	AuthToken    string   `json:"auth_token"`
	LogLevel     string   `json:"log_level"`
}

func LoadConfig() ServerConfig {
	cfg := ServerConfig{
		Port:         8080,
		DefaultLimit: 100,
		Algorithms: []string{
			"fixed_window",
			"sliding_window_counter",
			"sliding_window_log",
			"token_bucket",
			"leaky_bucket",
		},
	}

	if portStr := os.Getenv("RLIMITER_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.Port = port
		}
	}

	if limitStr := os.Getenv("RLIMITER_DEFAULT_LIMIT"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			cfg.DefaultLimit = limit
		}
	}

	if algosStr := os.Getenv("RLIMITER_ALGORITHMS"); algosStr != "" {
		cfg.Algorithms = strings.Split(algosStr, ",")
	}

	cfg.AuthToken = os.Getenv("RLIMITER_AUTH_TOKEN")
	cfg.LogLevel = os.Getenv("RLIMITER_LOG_LEVEL")

	return cfg
}

func NewLogger(level string) logging.Logger {
	switch level {
	case "info", "debug", "warn", "error":
		return &logging.SimpleLogger{}
	default:
		return &logging.NoOpLogger{}
	}
}
