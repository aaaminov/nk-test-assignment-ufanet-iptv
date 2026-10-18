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
	HTTPAddr string
	LogLevel slog.Level
}

// Load reads process settings. Unset variables use defaults; empty values are invalid.
func Load() (Config, error) {
	cfg := Config{HTTPAddr: ":8080", LogLevel: slog.LevelInfo}
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
	return cfg, nil
}
