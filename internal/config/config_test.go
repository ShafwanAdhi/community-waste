package config

import (
	"strings"
	"testing"
	"time"
)

func setEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("APP_PORT", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("DATABASE_URL", "postgres://community:local@db:5432/community_waste?sslmode=disable")
}

func TestLoadDefaults(t *testing.T) {
	setEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: address=%s shutdown=%s", cfg.HTTPAddr, cfg.ShutdownTimeout)
	}
}

func TestLoadExplicitSettings(t *testing.T) {
	setEnvironment(t)
	t.Setenv("APP_PORT", "9000")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9000" || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("settings were not applied: address=%s shutdown=%s", cfg.HTTPAddr, cfg.ShutdownTimeout)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	for _, tc := range []struct{ name, key, value string }{
		{"missing database", "DATABASE_URL", ""},
		{"invalid database URL", "DATABASE_URL", "postgres://user:private-pass@db:bad/database"},
		{"wrong database scheme", "DATABASE_URL", "http://db/database"},
		{"missing database host", "DATABASE_URL", "postgres:///database"},
		{"missing database name", "DATABASE_URL", "postgres://db"},
		{"invalid port", "APP_PORT", "abc"},
		{"zero port", "APP_PORT", "0"},
		{"large port", "APP_PORT", "65536"},
		{"negative port", "APP_PORT", "-1"},
		{"invalid timeout", "SHUTDOWN_TIMEOUT", "tomorrow"},
		{"zero timeout", "SHUTDOWN_TIMEOUT", "0s"},
		{"negative timeout", "SHUTDOWN_TIMEOUT", "-1s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnvironment(t)
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil {
				t.Fatal("expected invalid configuration to be rejected")
			}
			if strings.Contains(err.Error(), "private-pass") {
				t.Fatal("configuration error exposed a password")
			}
		})
	}
}
