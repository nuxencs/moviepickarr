package tmdb

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrRuntimeDisabled = errors.New("TMDB integration is disabled")
	ErrAPIKeyRejected  = errors.New("API key rejected")
)

type RuntimeSnapshot struct {
	Revision int64
	Config   RuntimeConfig

	generation uint64
}

type RuntimeEffects struct {
	RefreshStale       bool
	Reschedule         bool
	NextScheduledCheck time.Time
}

type runtimeState struct {
	snapshot  RuntimeSnapshot
	suspended bool
}

type Runtime struct {
	state atomic.Pointer[runtimeState]
}

func NewRuntime(config RuntimeConfig, revision int64) *Runtime {
	runtime := &Runtime{}
	runtime.state.Store(&runtimeState{snapshot: RuntimeSnapshot{
		Revision:   revision,
		Config:     config,
		generation: 1,
	}})
	return runtime
}

func (r *Runtime) Acquire() (RuntimeSnapshot, error) {
	state := r.state.Load()
	snapshot := state.snapshot
	if !snapshot.Config.Enabled || snapshot.Config.APIKey == "" {
		return RuntimeSnapshot{}, ErrRuntimeDisabled
	}
	if state.suspended {
		return RuntimeSnapshot{}, ErrAPIKeyRejected
	}
	return snapshot, nil
}

func (r *Runtime) Replace(config RuntimeConfig, revision int64, replacedAt time.Time) RuntimeEffects {
	return r.replace(config, revision, replacedAt, false)
}

func (r *Runtime) ReplaceVerified(config RuntimeConfig, revision int64, replacedAt time.Time) RuntimeEffects {
	return r.replace(config, revision, replacedAt, true)
}

func (r *Runtime) replace(
	config RuntimeConfig,
	revision int64,
	replacedAt time.Time,
	credentialVerified bool,
) RuntimeEffects {
	for {
		current := r.state.Load()
		replacement := &runtimeState{snapshot: RuntimeSnapshot{
			Revision:   revision,
			Config:     config,
			generation: current.snapshot.generation + 1,
		}, suspended: current.suspended && current.snapshot.Config.APIKey == config.APIKey && !credentialVerified}
		if r.state.CompareAndSwap(current, replacement) {
			currentScheduleAvailable := runtimeScheduleAvailable(current.snapshot.Config, current.suspended)
			replacementScheduleAvailable := runtimeScheduleAvailable(config, replacement.suspended)
			effects := RuntimeEffects{
				RefreshStale: !runtimeConfigAvailable(current.snapshot.Config) &&
					runtimeConfigAvailable(config) && !replacement.suspended,
			}
			if current.snapshot.Config.RefreshInterval != config.RefreshInterval ||
				currentScheduleAvailable != replacementScheduleAvailable {
				effects.Reschedule = true
				if replacementScheduleAvailable {
					effects.NextScheduledCheck = replacedAt.Add(config.RefreshInterval)
				}
			}
			return effects
		}
	}
}

func runtimeConfigAvailable(config RuntimeConfig) bool {
	return config.Enabled && config.APIKey != ""
}

func runtimeScheduleAvailable(config RuntimeConfig, suspended bool) bool {
	return runtimeConfigAvailable(config) && config.RefreshInterval > 0 && !suspended
}

func (r *Runtime) AuthenticationRejected(snapshot RuntimeSnapshot) bool {
	for {
		current := r.state.Load()
		if snapshot.generation == 0 || current.snapshot.generation != snapshot.generation {
			return false
		}
		if current.suspended {
			return true
		}
		suspended := &runtimeState{snapshot: current.snapshot, suspended: true}
		if r.state.CompareAndSwap(current, suspended) {
			return true
		}
	}
}

func (r *Runtime) ConnectionSucceeded(revision int64, testedAPIKey string) bool {
	for {
		current := r.state.Load()
		if current.snapshot.Revision != revision || current.snapshot.Config.APIKey != testedAPIKey {
			return false
		}
		if !current.suspended {
			return false
		}
		resumedSnapshot := current.snapshot
		resumedSnapshot.generation++
		if r.state.CompareAndSwap(current, &runtimeState{snapshot: resumedSnapshot}) {
			return true
		}
	}
}
