package integration

import (
	"context"
	"errors"
	"time"
)

var ErrStaleRevision = errors.New("integration configuration revision is stale")

type State string

const (
	StateDisabled              State = "disabled"
	StateConnected             State = "connected"
	StateCouldNotVerify        State = "could_not_verify"
	StateError                 State = "error"
	StateCredentialUnavailable State = "credential_unavailable"
)

type ConfigRecord struct {
	Integration            string
	Revision               int64
	AdminConfig            []byte
	EncryptedSecret        []byte
	State                  State
	StateReason            string
	LastCheckedAt          *time.Time
	LastConnectionTestedAt *time.Time
	NextCheckAt            *time.Time
	LastSuccessfulRunAt    *time.Time
	UpdatedAt              time.Time
}

type SecretAction int

const (
	SecretPreserve SecretAction = iota
	SecretReplace
	SecretClear
)

type ConfigSave struct {
	Integration        string
	ExpectedRevision   int64
	AdminConfig        []byte
	SecretAction       SecretAction
	EncryptedSecret    []byte
	State              State
	StateReason        string
	ConnectionTestedAt *time.Time
}

type ConfigStore interface {
	Get(context.Context, string) (ConfigRecord, error)
	Save(context.Context, ConfigSave) (ConfigRecord, error)
	UpdateState(context.Context, string, State, string) error
	UpdateConnectionTest(context.Context, string, State, string, time.Time) error
	UpdateLastChecked(context.Context, string, time.Time) error
	UpdateNextCheck(context.Context, string, *time.Time) error
	UpdateSchedule(context.Context, string, time.Time, *time.Time) error
	UpdateSuccessfulRun(context.Context, string, time.Time) error
}
