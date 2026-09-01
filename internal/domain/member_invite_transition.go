package domain

import (
	"context"
	"time"
)

// MemberInviteGeneration is the persisted half of a freshly generated claim
// link. The raw token stays in the use-case layer and is returned only to the
// admin whose request commits this generation.
type MemberInviteGeneration struct {
	PublicID  string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
	CreatedBy int
}

// MemberInviteTransitionStore owns membership lifecycle writes that must land
// with a fresh onboarding invite. Both methods leave no partial member state
// when any invite write fails.
type MemberInviteTransitionStore interface {
	CreateMemberWithInvite(ctx context.Context, name string, role Role, invite MemberInviteGeneration) (*User, error)
	RestoreMemberWithInvite(ctx context.Context, userID int, invite MemberInviteGeneration) (*User, error)
}

// InviteTransitionStore is the complete persistence port consumed by the
// invite manager: credential transitions plus member onboarding transitions.
type InviteTransitionStore interface {
	AuthTransitionStore
	MemberInviteTransitionStore
}
