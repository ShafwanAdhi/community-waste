package database

import (
	"errors"
	"fmt"
	"time"

	"community-waste/migrations"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func Migrate(databaseURL, direction string) (result error) {
	if direction != "up" && direction != "down" {
		return errors.New("migration direction must be up or down")
	}
	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid database connection configuration")
	}
	cfg.ConnectTimeout = 5 * time.Second
	db := stdlib.OpenDB(*cfg)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{StatementTimeout: 10 * time.Second})
	if err != nil {
		db.Close()
		return fmt.Errorf("initialize migration database: %w", err)
	}
	source, err := iofs.New(migrations.Files, ".")
	if err != nil {
		driver.Close()
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	runner, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		source.Close()
		driver.Close()
		return fmt.Errorf("initialize migrations: %w", err)
	}
	// Driver and source ownership transfers to the runner after initialization.
	defer func() {
		sourceErr, databaseErr := runner.Close()
		result = errors.Join(result, sourceErr, databaseErr)
	}()
	if direction == "up" {
		err = runner.Up()
	} else {
		err = runner.Steps(-1)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %s: %w", direction, err)
	}
	return nil
}
