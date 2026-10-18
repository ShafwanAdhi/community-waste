package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"community-waste/internal/config"
	"community-waste/internal/database"
	"community-waste/internal/handler"
	"community-waste/internal/repository/postgres"
	"community-waste/internal/server"
	"community-waste/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], logger)
	stop()
	if err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, logger *slog.Logger) error {
	command := "serve"
	if len(args) > 0 {
		command = args[0]
	}
	valid := (command == "serve" && len(args) <= 1) ||
		(command == "healthcheck" && len(args) == 1) ||
		(command == "migrate" && len(args) == 2 && (args[1] == "up" || args[1] == "down"))
	if !valid {
		return errors.New("usage: community-waste [serve | healthcheck | migrate up | migrate down]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if command == "migrate" {
		if err := database.Migrate(cfg.DatabaseURL, args[1]); err != nil {
			return err
		}
		logger.Info("migrations applied", "direction", args[1])
		return nil
	}
	if command == "healthcheck" {
		client := &http.Client{Timeout: 2500 * time.Millisecond}
		response, err := client.Get("http://127.0.0.1" + cfg.HTTPAddr + "/healthz")
		if err != nil {
			return fmt.Errorf("health check: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("health check returned HTTP %d", response.StatusCode)
		}
		return nil
	}
	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pool, err := database.Open(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	households := service.NewHousehold(postgres.NewHousehold(pool))
	pickups := service.NewPickup(postgres.NewPickup(pool))
	billing := service.NewBilling(postgres.NewBilling(pool))
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.New(pool.Ping, logger, households, pickups, billing),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("HTTP server listening", "address", listener.Addr().String())
	if err := server.Serve(ctx, srv, listener, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info("HTTP server stopped")
	return nil
}
