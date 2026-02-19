package domain

import (
	"context"
	"time"
)

type UserRepo interface {
	FindByID(ctx context.Context, id int) (*User, error)
	List(ctx context.Context) ([]*User, error)
	Create(ctx context.Context, name string) (*User, error)
	Delete(ctx context.Context, id int) error
}

type User struct {
	ID         int
	Name       string
	Username   string
	Role       UserRole
	HasAccount bool
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}
