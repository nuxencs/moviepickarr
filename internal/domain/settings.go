package domain

import "context"

type SettingsRepo interface {
	List(ctx context.Context) ([]*Settings, error)
	FindByKey(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string) error
}

type Settings struct {
	Key   string
	Value string
}
