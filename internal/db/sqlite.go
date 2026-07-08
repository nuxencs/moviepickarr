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

// Pool separates the single writer connection from a small pool of readers.
// WAL lets readers run concurrently with the writer, but writes must stay
// serialized on one connection so two goroutines never contend for the write
// lock mid-transaction (SQLITE_BUSY). Reads route to Read, mutations to Write.
type Pool struct {
	Read  *sql.DB
	Write *sql.DB
}

func OpenSQLite(path string) (*Pool, error) {
	dsn := sqliteDSN(path)

	// The writer opens first: on a fresh file it creates the DB and switches it
	// to WAL before any reader connects.
	write, err := openHandle(dsn, 1)
	if err != nil {
		return nil, err
	}

	// query_only turns a mis-routed write on the read pool into an immediate,
	// loud error instead of silent write contention. Appended last so the
	// earlier pragmas (journal_mode etc.) still apply; WAL is already set
	// persistently by the writer, so the reader's journal_mode is a no-op read.
	read, err := openHandle(dsn+"&_pragma=query_only(1)", defaultReadConns)
	if err != nil {
		_ = write.Close()
		return nil, err
	}

	return &Pool{Read: read, Write: write}, nil
}

func (p *Pool) Close() error {
	// PRAGMA optimize is SQLite's recommended pre-close hygiene: it refreshes
	// the stats the query planner relies on (cheap and bounded on the writer).
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
