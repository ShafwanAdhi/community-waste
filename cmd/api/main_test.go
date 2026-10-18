package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestRunRejectsInvalidCommandBeforeLoadingConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, args := range [][]string{{"unknown"}, {"serve", "extra"}, {"migrate"}, {"migrate", "reset"}, {"healthcheck", "extra"}} {
		if err := run(context.Background(), args, logger); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("args=%v: expected usage error, got %v", args, err)
		}
	}
}
