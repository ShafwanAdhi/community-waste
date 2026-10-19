package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	UploadDir       string
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		UploadDir:       "uploads",
		HTTPAddr:        ":8080",
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ShutdownTimeout: 10 * time.Second,
	}
	if root := os.Getenv("UPLOAD_DIR"); root != "" {

		cfg.UploadDir = root
	}
	databaseURL, err := url.Parse(cfg.DatabaseURL)
	if err != nil || databaseURL.Hostname() == "" || strings.Trim(databaseURL.Path, "/") == "" ||
		(databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") {
		return Config{}, errors.New("DATABASE_URL must be a PostgreSQL URL with a host and database name")
	}
	if port := os.Getenv("APP_PORT"); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return Config{}, errors.New("APP_PORT must be between 1 and 65535")
		}
		cfg.HTTPAddr = ":" + strconv.Itoa(number)
	}
	if value := os.Getenv("SHUTDOWN_TIMEOUT"); value != "" {
		cfg.ShutdownTimeout, err = time.ParseDuration(value)
		if err != nil || cfg.ShutdownTimeout <= 0 {
			return Config{}, errors.New("SHUTDOWN_TIMEOUT must be a positive duration")
		}
	}
	return cfg, nil
}
