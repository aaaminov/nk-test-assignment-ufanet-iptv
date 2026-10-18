package config

import (
	"errors"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	LogLevel    slog.Level
	DatabaseURL string
	DBMaxConns  int32
}

// Load reads process settings. Unset optional variables use defaults; empty values are invalid.
func Load() (Config, error) {
	cfg := Config{HTTPAddr: ":8080", LogLevel: slog.LevelInfo, DBMaxConns: 10}
	if addr, ok := os.LookupEnv("HTTP_ADDR"); ok {
		cfg.HTTPAddr = addr
	}
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil || strings.ContainsAny(host, " \t\r\n/") {
		return Config{}, errors.New("HTTP_ADDR must be host:port with a numeric port from 0 to 65535")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return Config{}, errors.New("HTTP_ADDR must be host:port with a numeric port from 0 to 65535")
	}

	if level, ok := os.LookupEnv("LOG_LEVEL"); ok {
		switch strings.ToLower(level) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			return Config{}, errors.New("LOG_LEVEL must be debug, info, warn or error")
		}
	}
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if value, ok := os.LookupEnv("DB_MAX_CONNS"); ok {
		maxConns, err := strconv.ParseInt(value, 10, 32)
		if err != nil || maxConns <= 0 {
			return Config{}, errors.New("DB_MAX_CONNS must be a positive int32")
		}
		cfg.DBMaxConns = int32(maxConns)
	}
	return cfg, nil
}
