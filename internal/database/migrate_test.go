package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMigrateRejectsUnknownDirection(t *testing.T) {
	if err := Migrate("", "reset"); err == nil {
		t.Fatal("unknown direction must fail before connecting")
	}
}

func TestMigrationsRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.Database, "_test") {
		t.Fatal("integration tests require a database ending in _test")
	}
	ctx := t.Context()
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	schema := fmt.Sprintf("migration_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	migrationURL := databaseURL + separator + "search_path=" + schema

	if err := Migrate(migrationURL, "up"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(migrationURL, "up"); err != nil {
		t.Fatalf("second up: %v", err)
	}
	pool, err := Open(ctx, migrationURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const household = "10000000-0000-4000-8000-000000000001"
	const other = "10000000-0000-4000-8000-000000000002"
	const pickup = "20000000-0000-4000-8000-000000000001"
	const payment = "30000000-0000-4000-8000-000000000001"
	for _, statement := range []string{
		"INSERT INTO households(id,owner_name,address) VALUES ('" + household + "','Ayu','Jl. Merdeka 1'),('" + other + "','Budi','Jl. Merdeka 2')",
		"INSERT INTO pickups(id,household_id,type) VALUES ('" + pickup + "','" + household + "','organic')",
		"INSERT INTO payments(id,household_id,waste_id,amount) VALUES ('" + payment + "','" + household + "','" + pickup + "',50000.00)",
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ name, sql, state string }{
		{"empty owner", "UPDATE households SET owner_name='  ' WHERE id='" + household + "'", "23514"},
		{"unknown waste type", "UPDATE pickups SET type='metal' WHERE id='" + pickup + "'", "23514"},
		{"unknown pickup status", "UPDATE pickups SET status='lost' WHERE id='" + pickup + "'", "23514"},
		{"missing electronic safety", "UPDATE pickups SET type='electronic', safety_check=NULL WHERE id='" + pickup + "'", "23514"},
		{"unknown payment status", "UPDATE payments SET status='refunded' WHERE id='" + payment + "'", "23514"},
		{"zero amount", "UPDATE payments SET amount=0 WHERE id='" + payment + "'", "23514"},
		{"negative amount", "UPDATE payments SET amount=-1 WHERE id='" + payment + "'", "23514"},
		{"NaN amount", "UPDATE payments SET amount='NaN' WHERE id='" + payment + "'", "23514"},
		{"household mismatch", "UPDATE payments SET household_id='" + other + "' WHERE id='" + payment + "'", "23503"},
		{"duplicate payment", "INSERT INTO payments(id,household_id,waste_id,amount) VALUES ('30000000-0000-4000-8000-000000000002','" + household + "','" + pickup + "',50000.00)", "23505"},
		{"referenced household delete", "DELETE FROM households WHERE id='" + household + "'", "23001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			_, err = tx.Exec(t.Context(), tc.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.state {
				t.Fatalf("expected SQLSTATE %s, got %v", tc.state, err)
			}
		})
	}
	if err := Migrate(migrationURL, "down"); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('households') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("down did not remove household table")
	}
	if err := Migrate(migrationURL, "up"); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM households").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected new empty schema, got %d households", count)
	}
}
