package config

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"HTTP_ADDR", "LOG_LEVEL"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	for _, tc := range []struct {
		addr  string
		level string
		want  slog.Level
	}{
		{"127.0.0.1:9000", "debug", slog.LevelDebug},
		{"[::1]:8081", "INFO", slog.LevelInfo},
		{"localhost:0", "warn", slog.LevelWarn},
		{":65535", "error", slog.LevelError},
	} {
		t.Run(tc.level, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", tc.addr)
			t.Setenv("LOG_LEVEL", tc.level)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr != tc.addr || cfg.LogLevel != tc.want {
				t.Fatalf("unexpected configuration: %+v", cfg)
			}
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{"HTTP_ADDR", ""},
		{"HTTP_ADDR", "localhost"},
		{"HTTP_ADDR", "http://localhost:8080"},
		{"HTTP_ADDR", ":http"},
		{"HTTP_ADDR", ":65536"},
		{"HTTP_ADDR", ":-1"},
		{"HTTP_ADDR", ":8080 "},
		{"HTTP_ADDR", " localhost:8080"},
		{"LOG_LEVEL", ""},
		{"LOG_LEVEL", "trace"},
		{"LOG_LEVEL", "info+1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", ":8080")
			t.Setenv("LOG_LEVEL", "info")
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected an error naming %s, got %v", tc.key, err)
			}
		})
	}
}
