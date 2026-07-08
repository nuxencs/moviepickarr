package db

import (
	"errors"

	sqlite "modernc.org/sqlite"
)

// SQLite extended result codes (https://sqlite.org/rescode.html). Declared
// here so callers match on the typed driver error instead of its message
// text, which is not stable across driver versions.
const (
	sqliteConstraintForeignKey = 787
	sqliteConstraintTrigger    = 1811 // ON DELETE RESTRICT fires as a trigger constraint
	sqliteConstraintUnique     = 2067
	sqliteConstraintPrimaryKey = 1555
)

func sqliteErrCode(err error) int {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code()
	}
	return 0
}

// IsForeignKeyViolation reports whether err is a foreign-key constraint
// failure. Immediate FK violations report SQLITE_CONSTRAINT_FOREIGNKEY;
// RESTRICT-action rejections report SQLITE_CONSTRAINT_TRIGGER (pinned by
// TestConstraintErrorMatchers against the real driver).
func IsForeignKeyViolation(err error) bool {
	code := sqliteErrCode(err)
	return code == sqliteConstraintForeignKey || code == sqliteConstraintTrigger
}

// IsUniqueViolation reports whether err is a UNIQUE (or primary-key)
// constraint failure.
func IsUniqueViolation(err error) bool {
	code := sqliteErrCode(err)
	return code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey
}
