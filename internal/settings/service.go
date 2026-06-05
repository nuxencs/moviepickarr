package settings

import (
	"context"
	"fmt"
	"strconv"

	"moviepickarr/internal/domain"
)

type Service interface {
	GetPoolLock(ctx context.Context) (bool, error)
	SetPoolLock(ctx context.Context, value bool) error
}

type service struct {
	settingsRepo domain.SettingsRepo
}

func NewService(settingsRepo domain.SettingsRepo) Service {
	return &service{settingsRepo: settingsRepo}
}

func (s *service) GetPoolLock(ctx context.Context) (bool, error) {
	poolLock, err := s.settingsRepo.FindByKey(ctx, "pool_locked")
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(poolLock)
}

func (s *service) SetPoolLock(ctx context.Context, value bool) error {
	return s.settingsRepo.Set(ctx, "pool_locked", fmt.Sprint(value))
}
