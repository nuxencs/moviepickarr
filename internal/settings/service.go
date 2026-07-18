package settings

import (
	"context"
	"fmt"
	"strconv"

	"moviepickarr/internal/domain"
)

// Service hides the settings key names and their string encoding behind typed
// accessors.
type Service struct {
	settingsRepo domain.SettingsRepo
}

func NewService(settingsRepo domain.SettingsRepo) *Service {
	return &Service{settingsRepo: settingsRepo}
}

func (s *Service) GetPoolLock(ctx context.Context) (bool, error) {
	poolLock, err := s.settingsRepo.FindByKey(ctx, "pool_locked")
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(poolLock)
}

func (s *Service) SetPoolLock(ctx context.Context, value bool) error {
	return s.settingsRepo.Set(ctx, "pool_locked", fmt.Sprint(value))
}
