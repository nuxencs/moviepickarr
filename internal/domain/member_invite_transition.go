package domain

import (
	"context"
	"time"
)

// MemberInviteGeneration is the persisted half of a new claim link. The raw
// token goes only to the admin whose request commits it.
type MemberInviteGeneration struct {
	PublicID  string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	CreatedBy int
}

// MemberInviteTransitionStore commits member lifecycle writes together with
// their onboarding invite.
type MemberInviteTransitionStore interface {
	CreateMemberWithInvite(ctx context.Context, name string, role Role, invite MemberInviteGeneration) (*User, error)
	RestoreMemberWithInvite(ctx context.Context, userID int, invite MemberInviteGeneration) (*User, error)
}

// InviteTransitionStore is the persistence port of the invite manager.
type InviteTransitionStore interface {
	AuthTransitionStore
	MemberInviteTransitionStore
}
