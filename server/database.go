package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func openDatabase(dsn string) (*bun.DB, error) {
	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn)))
	db := bun.NewDB(sqldb, pgdialect.New())

	if err := db.PingContext(context.Background()); err != nil {
		_ = sqldb.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

func createTables(ctx context.Context, db *bun.DB) error {
	models := []any{
		(*User)(nil),
		(*Friendship)(nil),
		(*Block)(nil),
		(*Chat)(nil),
		(*ChatMember)(nil),
		(*ChatEpochKey)(nil),
		(*Message)(nil),
		(*AdminAuditLog)(nil),
	}

	for _, model := range models {
		if _, err := db.NewCreateTable().
			Model(model).
			IfNotExists().
			WithForeignKeys().
			Exec(ctx); err != nil {
			return fmt.Errorf("create %T table: %w", model, err)
		}
	}

	return nil
}
