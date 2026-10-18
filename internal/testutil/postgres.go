package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"community-waste/internal/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("invalid TEST_DATABASE_URL")
	}
	if !strings.HasSuffix(cfg.Database, "_test") {
		t.Fatal("integration tests require a database ending in _test")
	}
	admin, err := pgx.ConnectConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	connectionURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	query := connectionURL.Query()
	query.Set("search_path", schema)
	connectionURL.RawQuery = query.Encode()
	if err := database.Migrate(connectionURL.String(), "up"); err != nil {
		t.Fatal(err)
	}
	pool, err := database.Open(t.Context(), connectionURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
