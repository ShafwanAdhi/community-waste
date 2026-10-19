package main

import (
	"community-waste/internal/testutil"
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRunRejectsInvalidCommandBeforeLoadingConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, args := range [][]string{{"seed", "extra"}, {"unknown"}, {"serve", "extra"}, {"migrate"}, {"migrate", "reset"}, {"healthcheck", "extra"}} {
		if err := run(context.Background(), args, logger); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("args=%v: expected usage error, got %v", args, err)
		}
	}
}

func TestRunSeedDoesNotStartHTTPServer(t *testing.T) {
	pool := testutil.NewDatabase(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("DATABASE_URL", pool.Config().ConnString())
	t.Setenv("UPLOAD_DIR", t.TempDir())
	t.Setenv("APP_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for range 2 {
		if err := run(t.Context(), []string{"seed"}, logger); err != nil {
			t.Fatal(err)
		}
	}
	var households, pickups, payments int
	if err := pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM households),(SELECT count(*) FROM pickups),(SELECT count(*) FROM payments)").Scan(&households, &pickups, &payments); err != nil || households != 4 || pickups != 16 || payments != 6 {
		t.Fatalf("seed CLI: %d/%d/%d %v", households, pickups, payments, err)
	}
}
