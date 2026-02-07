package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"
)

const (
	defaultBusyTimeoutMS    = 5000
	defaultJournalSizeLimit = 64 * 1024 * 1024
)

func OpenSQLite(path string) (*sql.DB, error) {
	dsn := sqliteDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func sqliteDSN(path string) string {
	escaped := url.PathEscape(path)
	return fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(%d)&_pragma=journal_size_limit(%d)",
		escaped,
		defaultBusyTimeoutMS,
		defaultJournalSizeLimit,
	)
}
