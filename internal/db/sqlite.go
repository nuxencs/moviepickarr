package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite"
)

const (
	defaultBusyTimeoutMS    = 5000
	defaultJournalSizeLimit = 64 * 1024 * 1024
	defaultReadConns        = 4
)

// Pool splits one writer connection from a small read pool. One writer keeps
// two goroutines from contending for the write lock (SQLITE_BUSY).
type Pool struct {
	Read  *sql.DB
	Write *sql.DB
}

func OpenSQLite(path string) (*Pool, error) {
	dsn := sqliteDSN(path)

	// The writer opens first so a fresh file is in WAL before any reader connects.
	write, err := openHandle(dsn, 1)
	if err != nil {
		return nil, err
	}

	// query_only makes a mis-routed write fail loudly. Appended last so the
	// earlier pragmas still apply.
	read, err := openHandle(dsn+"&_pragma=query_only(1)", defaultReadConns)
	if err != nil {
		_ = write.Close()
		return nil, err
	}

	return &Pool{Read: read, Write: write}, nil
}

func (p *Pool) Close() error {
	// SQLite recommends PRAGMA optimize before close to refresh planner stats.
	_, _ = p.Write.Exec("PRAGMA optimize")
	return errors.Join(p.Write.Close(), p.Read.Close())
}

func openHandle(dsn string, maxConns int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)

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
