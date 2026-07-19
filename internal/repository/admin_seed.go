package repository

import (
	"context"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

// SqliteAdminSeedRepository backs the break-glass admin seed over the users +
// local_accounts tables. Reads route to the read pool and the two writes to the
// write pool, matching the single-writer discipline the other repositories
// follow.
type SqliteAdminSeedRepository struct {
	pool *db.Pool
}

func NewSqliteAdminSeedRepository(pool *db.Pool) *SqliteAdminSeedRepository {
	return &SqliteAdminSeedRepository{pool: pool}
}

// FindUsersByNameFold matches on name case-insensitively. users.name is a
// case-sensitive UNIQUE, so a NOCASE compare can still return several rows
// (e.g. "Bob" and "bob"): that plurality is exactly the ambiguity the seed
// checks for.
func (d *SqliteAdminSeedRepository) FindUsersByNameFold(ctx context.Context, name string) ([]domain.SeedUser, error) {
	query := "SELECT id, name, role FROM users WHERE name = ? COLLATE NOCASE ORDER BY id"

	rows, err := d.pool.Read.QueryContext(ctx, query, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]domain.SeedUser, 0)
	for rows.Next() {
		var u domain.SeedUser
		if err := rows.Scan(&u.ID, &u.Name, &u.Role); err != nil {
			return nil, err
		}
		users = append(users, u)
	}

	return users, rows.Err()
}

func (d *SqliteAdminSeedRepository) CreateAdmin(ctx context.Context, name string) (int, error) {
	result, err := d.pool.Write.ExecContext(ctx, "INSERT INTO users (name, role) VALUES (?, 'admin')", name)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return int(id), nil
}

func (d *SqliteAdminSeedRepository) PromoteToAdmin(ctx context.Context, id int) error {
	_, err := d.pool.Write.ExecContext(ctx, "UPDATE users SET role = 'admin' WHERE id = ?", id)
	return err
}

func (d *SqliteAdminSeedRepository) HasLocalAccount(ctx context.Context, userID int) (bool, error) {
	var exists int
	err := d.pool.Read.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM local_accounts WHERE user_id = ?)", userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (d *SqliteAdminSeedRepository) CreateLocalAccount(ctx context.Context, userID int, username, passwordHash string) error {
	_, err := d.pool.Write.ExecContext(ctx,
		"INSERT INTO local_accounts (user_id, username, password_hash) VALUES (?, ?, ?)",
		userID, username, passwordHash)
	return err
}

func (d *SqliteAdminSeedRepository) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := d.pool.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
