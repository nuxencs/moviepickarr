package server

import (
	"context"
	"fmt"
	"log"
	"strings"

	"moviepickarr/internal/domain"
)

func (h *handler) ensureInitialAdmin(ctx context.Context) error {
	hasAnyAccount, err := h.authService.HasAnyAccount(ctx)
	if err != nil {
		return err
	}
	if hasAnyAccount {
		return nil
	}

	username := strings.TrimSpace(h.authAdminUsername)
	password := strings.TrimSpace(h.authAdminPassword)
	if username == "" || password == "" {
		log.Printf("no local accounts found; set AUTH_ADMIN_USERNAME and AUTH_ADMIN_PASSWORD to bootstrap first admin")
		return nil
	}

	name := strings.TrimSpace(h.authAdminName)
	if name == "" {
		name = "Admin"
	}

	targetUserID, createdNewUser, err := h.resolveInitialAdminUser(ctx, name)
	if err != nil {
		return err
	}

	if _, err := h.authService.UpsertAccount(ctx, targetUserID, username, password, domain.RoleAdmin); err != nil {
		if createdNewUser {
			_ = h.userService.Delete(ctx, targetUserID)
		}
		return fmt.Errorf("seed admin account: %w", err)
	}

	log.Printf("seeded initial admin account for user %q", name)
	return nil
}

func (h *handler) resolveInitialAdminUser(ctx context.Context, name string) (int, bool, error) {
	users, err := h.userService.List(ctx)
	if err != nil {
		return 0, false, err
	}

	for _, candidate := range users {
		if strings.EqualFold(strings.TrimSpace(candidate.Name), name) {
			return candidate.ID, false, nil
		}
	}

	createdUser, err := h.userService.Create(ctx, name)
	if err != nil {
		return 0, false, err
	}

	return createdUser.ID, true, nil
}
