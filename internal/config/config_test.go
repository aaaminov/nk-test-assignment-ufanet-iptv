package config

import (
	"log/slog"
	"os"
	"strings"
	"testing"
)

const testDatabaseURL = "postgres://localhost/iptv_test?sslmode=disable"

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{"HTTP_ADDR", "LOG_LEVEL", "DB_MAX_CONNS"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DATABASE_URL", testDatabaseURL)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.DBMaxConns != 10 || cfg.DatabaseURL != testDatabaseURL {
		t.Fatal("unexpected default configuration")
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
			t.Setenv("DB_MAX_CONNS", "3")
			t.Setenv("DATABASE_URL", testDatabaseURL)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.HTTPAddr != tc.addr || cfg.LogLevel != tc.want || cfg.DatabaseURL != testDatabaseURL || cfg.DBMaxConns != 3 {
				t.Fatal("unexpected configuration overrides")
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
		{"DATABASE_URL", ""},
		{"DATABASE_URL", " \t\n"},
		{"DB_MAX_CONNS", ""},
		{"DB_MAX_CONNS", "0"},
		{"DB_MAX_CONNS", "-1"},
		{"DB_MAX_CONNS", "1.5"},
		{"DB_MAX_CONNS", "ten"},
		{"DB_MAX_CONNS", "2147483648"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv("HTTP_ADDR", ":8080")
			t.Setenv("LOG_LEVEL", "info")
			t.Setenv("DATABASE_URL", testDatabaseURL)
			t.Setenv("DB_MAX_CONNS", "10")
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("expected an error naming %s, got %v", tc.key, err)
			}
		})
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("DB_MAX_CONNS", "10")
	t.Setenv("DATABASE_URL", "")
	if err := os.Unsetenv("DATABASE_URL"); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected missing DATABASE_URL error, got %v", err)
	}
}
