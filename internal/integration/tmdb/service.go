package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"moviepickarr/internal/integration"
)

const (
	integrationName      = "tmdb"
	apiKeyRejectedReason = "API key rejected"
	unverifiedReason     = "TMDB connection has not been verified."
)

type SecretCodec interface {
	Encrypt(string) ([]byte, error)
	Decrypt([]byte) (string, error)
}

var ErrAuthentication = errors.New("TMDB authentication failed")

type RuntimeConfig struct {
	Enabled         bool
	APIKey          string
	CastLimit       int
	RefreshInterval time.Duration
	TTL             time.Duration
	MinInterval     time.Duration
	MaxRetries      int
	Backoff         time.Duration
	BatchLimit      int
}

type ConnectionTester interface {
	TestConnection(context.Context, RuntimeConfig) error
}

type RuntimeUpdater interface {
	Acquire() (RuntimeSnapshot, error)
	Replace(RuntimeConfig, int64, time.Time) RuntimeEffects
	ReplaceVerified(RuntimeConfig, int64, time.Time) RuntimeEffects
	AuthenticationRejected(RuntimeSnapshot) bool
	ConnectionSucceeded(int64, string) bool
}

type Service struct {
	store       integration.ConfigStore
	secrets     SecretCodec
	environment EnvironmentConfig
	tester      ConnectionTester
	runtime     RuntimeUpdater

	runtimeLoadMu sync.Mutex
	runtimeState  atomic.Pointer[loadedRuntimeState]
}

type loadedRuntimeState struct {
	revision              int64
	apiKey                string
	credentialUnavailable bool
}

type ConfigView struct {
	Revision               int64
	Config                 ResolvedConfig
	State                  integration.State
	Reason                 string
	LastCheckedAt          *time.Time
	LastConnectionTestedAt *time.Time
	NextCheckAt            *time.Time
	LastSuccessfulRunAt    *time.Time
	Effects                RuntimeEffects
}

type SaveDraft struct {
	Revision        int64
	Admin           AdminConfig
	RemoveFallbacks []string
	APIKey          string
	ClearAPIKey     bool
	ConfirmWarnings bool
}

type ConnectionResult struct {
	State           integration.State
	Reason          string
	CheckedAt       time.Time
	RuntimeResumed  bool
	RuntimeRevision int64
}

func NewService(
	store integration.ConfigStore,
	secrets SecretCodec,
	environment EnvironmentConfig,
	tester ConnectionTester,
	runtime RuntimeUpdater,
) *Service {
	if runtime == nil {
		runtime = NewRuntime(RuntimeConfig{}, 0)
	}
	return &Service{
		store:       store,
		secrets:     secrets,
		environment: environment,
		tester:      tester,
		runtime:     runtime,
	}
}

func (s *Service) Acquire(ctx context.Context) (RuntimeSnapshot, error) {
	state := s.runtimeState.Load()
	if state == nil {
		record, admin, err := s.load(ctx)
		if err != nil {
			return RuntimeSnapshot{}, err
		}
		state, _, err = s.ensureRuntime(record, admin)
		if err != nil {
			return RuntimeSnapshot{}, err
		}
	}
	if state.credentialUnavailable {
		return RuntimeSnapshot{}, integration.ErrCredentialUnavailable
	}
	return s.runtime.Acquire()
}

func (s *Service) AuthenticationRejected(ctx context.Context, snapshot RuntimeSnapshot) (bool, error) {
	s.runtimeLoadMu.Lock()
	defer s.runtimeLoadMu.Unlock()
	if !s.runtime.AuthenticationRejected(snapshot) {
		return false, nil
	}
	if err := s.store.UpdateState(
		ctx,
		integrationName,
		integration.StateError,
		apiKeyRejectedReason,
	); err != nil {
		return true, fmt.Errorf("record TMDB authentication rejection: %w", err)
	}
	return true, nil
}

func (s *Service) Get(ctx context.Context) (ConfigView, error) {
	record, admin, err := s.load(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	resolved := ResolveConfig(s.environment, admin, len(record.EncryptedSecret) > 0)
	runtimeState, _, err := s.ensureRuntime(record, admin)
	if err != nil {
		return ConfigView{}, err
	}
	state := record.State
	reason := record.StateReason
	if runtimeState.revision == record.Revision && runtimeState.credentialUnavailable {
		state = integration.StateCredentialUnavailable
		reason = "Stored API key cannot be decrypted with the current instance key."
	}
	_, runtimeErr := s.runtime.Acquire()
	if state == integration.StateError &&
		reason == apiKeyRejectedReason &&
		resolved.APIKey.Source == integration.SourceEnvironment &&
		!errors.Is(runtimeErr, ErrAPIKeyRejected) {
		state = integration.StateCouldNotVerify
		reason = unverifiedReason
	}
	if state == integration.StateDisabled && resolved.Enabled.Value && resolved.APIKey.Configured {
		state = integration.StateCouldNotVerify
		reason = unverifiedReason
	}
	if !resolved.Enabled.Value {
		state = integration.StateDisabled
		reason = ""
	}
	return ConfigView{
		Revision:               record.Revision,
		Config:                 resolved,
		State:                  state,
		Reason:                 reason,
		LastCheckedAt:          record.LastCheckedAt,
		LastConnectionTestedAt: record.LastConnectionTestedAt,
		NextCheckAt:            record.NextCheckAt,
		LastSuccessfulRunAt:    record.LastSuccessfulRunAt,
	}, nil
}

func (s *Service) ensureRuntime(
	record integration.ConfigRecord,
	admin AdminConfig,
) (*loadedRuntimeState, RuntimeEffects, error) {
	if state := s.runtimeState.Load(); state != nil && state.revision >= record.Revision {
		return state, RuntimeEffects{}, nil
	}

	s.runtimeLoadMu.Lock()
	defer s.runtimeLoadMu.Unlock()
	if state := s.runtimeState.Load(); state != nil && state.revision >= record.Revision {
		return state, RuntimeEffects{}, nil
	}

	apiKey := s.environment.APIKey
	credentialUnavailable := false
	if apiKey == "" && len(record.EncryptedSecret) > 0 {
		if s.secrets == nil {
			credentialUnavailable = true
		} else {
			var err error
			apiKey, err = s.secrets.Decrypt(record.EncryptedSecret)
			credentialUnavailable = err != nil
			if credentialUnavailable {
				apiKey = ""
			}
		}
	}
	resolved := ResolveConfig(s.environment, admin, len(record.EncryptedSecret) > 0)
	effects := s.runtime.Replace(runtimeConfig(resolved, apiKey), record.Revision, time.Now().UTC())
	if record.State == integration.StateError &&
		record.StateReason == apiKeyRejectedReason &&
		s.environment.APIKey == "" {
		if snapshot, err := s.runtime.Acquire(); err == nil {
			s.runtime.AuthenticationRejected(snapshot)
		}
	}
	state := &loadedRuntimeState{
		revision:              record.Revision,
		apiKey:                apiKey,
		credentialUnavailable: credentialUnavailable,
	}
	s.runtimeState.Store(state)
	return state, effects, nil
}

func (s *Service) Save(ctx context.Context, draft SaveDraft) (ConfigView, error) {
	record, current, err := s.load(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	if record.Revision != draft.Revision {
		return ConfigView{}, integration.ErrStaleRevision
	}
	admin, mergeIssues := mergeAdminConfig(current, draft.Admin, s.environment, draft.RemoveFallbacks)
	if issues := append(mergeIssues, ValidateAdminConfig(admin)...); len(issues) > 0 {
		return ConfigView{}, &ValidationError{Issues: issues}
	}
	if warnings := saveWarnings(s.environment, admin); len(warnings) > 0 && !draft.ConfirmWarnings {
		return ConfigView{}, &WarningConfirmationError{Warnings: warnings}
	}
	loadedRuntime, _, err := s.ensureRuntime(record, current)
	if err != nil {
		return ConfigView{}, err
	}
	encoded, err := json.Marshal(admin)
	if err != nil {
		return ConfigView{}, fmt.Errorf("encode TMDB Admin config: %w", err)
	}
	previousResolved := ResolveConfig(s.environment, current, len(record.EncryptedSecret) > 0)
	resolved := ResolveConfig(s.environment, admin, len(record.EncryptedSecret) > 0)
	state := integration.StateDisabled
	stateReason := record.StateReason
	secretAction := integration.SecretPreserve
	var encryptedSecret []byte
	runtimeAPIKey := loadedRuntime.apiKey
	credentialUnavailable := loadedRuntime.credentialUnavailable
	verifiedReplacement := false
	var connectionCheckedAt *time.Time
	if draft.ClearAPIKey && draft.APIKey != "" {
		return ConfigView{}, &ValidationError{Issues: []ValidationIssue{{
			Field:   "apiKey",
			Message: "cannot be replaced and cleared in the same save",
		}}}
	}
	newAPIKey := strings.TrimSpace(draft.APIKey)
	if draft.ClearAPIKey {
		secretAction = integration.SecretClear
		resolved = ResolveConfig(s.environment, admin, false)
		stateReason = ""
		runtimeAPIKey = s.environment.APIKey
		credentialUnavailable = false
		if resolved.Enabled.Value && resolved.APIKey.Configured {
			state = record.State
		} else {
			state = integration.StateDisabled
		}
	} else if newAPIKey != "" {
		if s.environment.APIKey != "" {
			return ConfigView{}, &ValidationError{Issues: []ValidationIssue{{
				Field:   "apiKey",
				Message: "is controlled by the environment",
			}}}
		}
		if s.tester == nil {
			return ConfigView{}, fmt.Errorf("TMDB connection tester is unavailable")
		}
		candidate := ResolveConfig(s.environment, admin, true)
		stateReason = ""
		checkedAt := time.Now().UTC()
		connectionErr := s.tester.TestConnection(ctx, runtimeConfig(candidate, newAPIKey))
		if errors.Is(connectionErr, ErrAuthentication) {
			return ConfigView{}, &AuthenticationError{}
		}
		state = integration.StateConnected
		connectionCheckedAt = &checkedAt
		verifiedReplacement = connectionErr == nil
		if connectionErr != nil {
			state = integration.StateCouldNotVerify
			stateReason = "TMDB could not be reached while checking the API key."
		}
		if s.secrets == nil {
			return ConfigView{}, fmt.Errorf("integration secret store is unavailable")
		}
		encryptedSecret, err = s.secrets.Encrypt(newAPIKey)
		if err != nil {
			return ConfigView{}, fmt.Errorf("encrypt TMDB API key: %w", err)
		}
		secretAction = integration.SecretReplace
		resolved = candidate
		runtimeAPIKey = newAPIKey
		credentialUnavailable = false
	} else if resolved.Enabled.Value && resolved.APIKey.Configured {
		state = record.State
		if state == integration.StateDisabled {
			state = integration.StateCouldNotVerify
		}
	}
	if !resolved.Enabled.Value {
		state = integration.StateDisabled
		stateReason = ""
	}
	s.runtimeLoadMu.Lock()
	if newAPIKey == "" &&
		!draft.ClearAPIKey &&
		previousResolved.Enabled.Value &&
		resolved.Enabled.Value {
		latest, latestErr := s.store.Get(ctx, integrationName)
		if latestErr != nil {
			s.runtimeLoadMu.Unlock()
			return ConfigView{}, latestErr
		}
		state = latest.State
		stateReason = latest.StateReason
	}
	saved, err := s.store.Save(ctx, integration.ConfigSave{
		Integration:        integrationName,
		ExpectedRevision:   draft.Revision,
		AdminConfig:        encoded,
		SecretAction:       secretAction,
		EncryptedSecret:    encryptedSecret,
		State:              state,
		StateReason:        stateReason,
		ConnectionTestedAt: connectionCheckedAt,
	})
	if err != nil {
		s.runtimeLoadMu.Unlock()
		return ConfigView{}, err
	}
	replacementConfig := runtimeConfig(resolved, runtimeAPIKey)
	replacementTime := time.Now().UTC()
	var effects RuntimeEffects
	if verifiedReplacement {
		effects = s.runtime.ReplaceVerified(replacementConfig, saved.Revision, replacementTime)
	} else {
		effects = s.runtime.Replace(replacementConfig, saved.Revision, replacementTime)
	}
	s.runtimeState.Store(&loadedRuntimeState{
		revision:              saved.Revision,
		apiKey:                runtimeAPIKey,
		credentialUnavailable: credentialUnavailable,
	})
	s.runtimeLoadMu.Unlock()
	view, err := s.Get(ctx)
	if err != nil {
		return ConfigView{}, err
	}
	view.Effects = effects
	return view, nil
}

func (s *Service) TestConnection(ctx context.Context, draft SaveDraft) (ConnectionResult, error) {
	record, current, err := s.load(ctx)
	if err != nil {
		return ConnectionResult{}, err
	}
	if record.Revision != draft.Revision {
		return ConnectionResult{}, integration.ErrStaleRevision
	}
	admin, mergeIssues := mergeAdminConfig(current, draft.Admin, s.environment, draft.RemoveFallbacks)
	if issues := append(mergeIssues, ValidateAdminConfig(admin)...); len(issues) > 0 {
		return ConnectionResult{}, &ValidationError{Issues: issues}
	}
	loadedRuntime, _, err := s.ensureRuntime(record, current)
	if err != nil {
		return ConnectionResult{}, err
	}

	apiKey := s.environment.APIKey
	testsSavedCredential := strings.TrimSpace(draft.APIKey) == "" && !draft.ClearAPIKey
	if draft.APIKey != "" {
		if apiKey != "" {
			return ConnectionResult{}, &ValidationError{Issues: []ValidationIssue{{
				Field:   "apiKey",
				Message: "is controlled by the environment",
			}}}
		}
		apiKey = strings.TrimSpace(draft.APIKey)
	} else if testsSavedCredential {
		if loadedRuntime.credentialUnavailable {
			return ConnectionResult{}, integration.ErrCredentialUnavailable
		}
		apiKey = loadedRuntime.apiKey
	}
	if apiKey == "" {
		return ConnectionResult{}, &ValidationError{Issues: []ValidationIssue{{
			Field:   "apiKey",
			Message: "is required to test the connection",
		}}}
	}
	if s.tester == nil {
		return ConnectionResult{}, fmt.Errorf("TMDB connection tester is unavailable")
	}
	resolved := ResolveConfig(s.environment, admin, apiKey != "")
	checkedAt := time.Now().UTC()
	var testedSnapshot RuntimeSnapshot
	if testsSavedCredential {
		testedSnapshot, _ = s.runtime.Acquire()
	}
	connectionErr := s.tester.TestConnection(ctx, runtimeConfig(resolved, apiKey))
	if errors.Is(connectionErr, ErrAuthentication) {
		if testsSavedCredential && testedSnapshot.generation != 0 {
			applied, err := s.AuthenticationRejected(ctx, testedSnapshot)
			if err != nil {
				return ConnectionResult{}, err
			}
			if applied {
				if err := s.store.UpdateConnectionTest(
					ctx,
					integrationName,
					integration.StateError,
					apiKeyRejectedReason,
					checkedAt,
				); err != nil {
					return ConnectionResult{}, fmt.Errorf("record rejected TMDB connection test: %w", err)
				}
			}
		}
		return ConnectionResult{
			State:           integration.StateError,
			Reason:          apiKeyRejectedReason,
			CheckedAt:       checkedAt,
			RuntimeRevision: testedSnapshot.Revision,
		}, nil
	}
	if connectionErr != nil {
		if testsSavedCredential {
			s.runtimeLoadMu.Lock()
			current := s.runtimeState.Load()
			if current != nil && current.revision == record.Revision && current.apiKey == apiKey {
				if err := s.store.UpdateConnectionTest(
					ctx,
					integrationName,
					integration.StateCouldNotVerify,
					"TMDB could not be reached.",
					checkedAt,
				); err != nil {
					s.runtimeLoadMu.Unlock()
					return ConnectionResult{}, fmt.Errorf("record unsuccessful TMDB connection test: %w", err)
				}
			}
			s.runtimeLoadMu.Unlock()
		}
		return ConnectionResult{
			State:           integration.StateCouldNotVerify,
			Reason:          "TMDB could not be reached.",
			CheckedAt:       checkedAt,
			RuntimeRevision: testedSnapshot.Revision,
		}, nil
	}
	runtimeResumed := false
	if testsSavedCredential {
		s.runtimeLoadMu.Lock()
		current := s.runtimeState.Load()
		if current != nil && current.revision == record.Revision && current.apiKey == apiKey {
			if err := s.store.UpdateConnectionTest(ctx, integrationName, integration.StateConnected, "", checkedAt); err != nil {
				s.runtimeLoadMu.Unlock()
				return ConnectionResult{}, fmt.Errorf("record successful TMDB connection test: %w", err)
			}
			runtimeResumed = s.runtime.ConnectionSucceeded(record.Revision, apiKey)
		}
		s.runtimeLoadMu.Unlock()
	}
	return ConnectionResult{
		State:           integration.StateConnected,
		CheckedAt:       checkedAt,
		RuntimeResumed:  runtimeResumed,
		RuntimeRevision: record.Revision,
	}, nil
}

func runtimeConfig(resolved ResolvedConfig, apiKey string) RuntimeConfig {
	return RuntimeConfig{
		Enabled:         resolved.Enabled.Value && apiKey != "",
		APIKey:          apiKey,
		CastLimit:       resolved.CastLimit.Value,
		RefreshInterval: resolved.RefreshInterval.Value,
		TTL:             resolved.TTL.Value,
		MinInterval:     resolved.MinInterval.Value,
		MaxRetries:      resolved.MaxRetries.Value,
		Backoff:         resolved.Backoff.Value,
		BatchLimit:      resolved.BatchLimit.Value,
	}
}

func mergeAdminConfig(
	current AdminConfig,
	draft AdminConfig,
	environment EnvironmentConfig,
	removeFields []string,
) (AdminConfig, []ValidationIssue) {
	remove := make(map[string]bool, len(removeFields))
	for _, field := range removeFields {
		remove[field] = true
	}
	issues := make([]ValidationIssue, 0)
	merged := draft
	merged.Enabled = mergeEnvironmentField("enabled", environment.Enabled != nil, current.Enabled, draft.Enabled, remove, &issues)
	merged.CastLimit = mergeEnvironmentField("castLimit", environment.CastLimit != nil, current.CastLimit, draft.CastLimit, remove, &issues)
	merged.RefreshInterval = mergeEnvironmentField("refreshInterval", environment.RefreshInterval != nil, current.RefreshInterval, draft.RefreshInterval, remove, &issues)
	merged.TTL = mergeEnvironmentField("ttl", environment.TTL != nil, current.TTL, draft.TTL, remove, &issues)
	merged.MinInterval = mergeEnvironmentField("minInterval", environment.MinInterval != nil, current.MinInterval, draft.MinInterval, remove, &issues)
	merged.MaxRetries = mergeEnvironmentField("maxRetries", environment.MaxRetries != nil, current.MaxRetries, draft.MaxRetries, remove, &issues)
	merged.Backoff = mergeEnvironmentField("backoff", environment.Backoff != nil, current.Backoff, draft.Backoff, remove, &issues)
	merged.BatchLimit = mergeEnvironmentField("batchLimit", environment.BatchLimit != nil, current.BatchLimit, draft.BatchLimit, remove, &issues)
	return merged, issues
}

func mergeEnvironmentField[T any](
	name string,
	environmentControlled bool,
	current *T,
	draft *T,
	remove map[string]bool,
	issues *[]ValidationIssue,
) *T {
	if !environmentControlled {
		return draft
	}
	if draft != nil {
		*issues = append(*issues, ValidationIssue{Field: name, Message: "is controlled by the environment"})
	}
	if remove[name] {
		return nil
	}
	return current
}

func saveWarnings(environment EnvironmentConfig, admin AdminConfig) []ConfigWarning {
	activeAdmin := admin
	if environment.RefreshInterval != nil {
		activeAdmin.RefreshInterval = nil
	}
	if environment.TTL != nil {
		activeAdmin.TTL = nil
	}
	if environment.MinInterval != nil {
		activeAdmin.MinInterval = nil
	}
	return ConfigWarnings(activeAdmin)
}

type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string { return "TMDB settings are invalid" }

type WarningConfirmationError struct {
	Warnings []ConfigWarning
}

func (e *WarningConfirmationError) Error() string {
	return "TMDB settings need confirmation"
}

type AuthenticationError struct{}

func (e *AuthenticationError) Error() string { return "TMDB rejected the API key" }

func (s *Service) load(ctx context.Context) (integration.ConfigRecord, AdminConfig, error) {
	record, err := s.store.Get(ctx, integrationName)
	if err != nil {
		return integration.ConfigRecord{}, AdminConfig{}, err
	}
	var admin AdminConfig
	if err := json.Unmarshal(record.AdminConfig, &admin); err != nil {
		return integration.ConfigRecord{}, AdminConfig{}, fmt.Errorf("decode TMDB Admin config: %w", err)
	}
	return record, admin, nil
}
