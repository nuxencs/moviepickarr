package db

import (
	"errors"

	sqlite "modernc.org/sqlite"
)

// SQLite extended result codes (https://sqlite.org/rescode.html). Match on
// these, not the message text, which changes between driver versions.
const (
	sqliteConstraintForeignKey = 787
	sqliteConstraintTrigger    = 1811 // ON DELETE RESTRICT fires as a trigger constraint
	sqliteConstraintUnique     = 2067
	sqliteConstraintPrimaryKey = 1555
)

func sqliteErrCode(err error) int {
	if se, ok := errors.AsType[*sqlite.Error](err); ok {
		return se.Code()
	}
	return 0
}

// IsForeignKeyViolation reports whether err is a foreign-key failure. RESTRICT
// rejections report SQLITE_CONSTRAINT_TRIGGER (see TestConstraintErrorMatchers).
func IsForeignKeyViolation(err error) bool {
	code := sqliteErrCode(err)
	return code == sqliteConstraintForeignKey || code == sqliteConstraintTrigger
}

// IsUniqueViolation reports whether err is a UNIQUE or primary-key failure.
func IsUniqueViolation(err error) bool {
	code := sqliteErrCode(err)
	return code == sqliteConstraintUnique || code == sqliteConstraintPrimaryKey
}
