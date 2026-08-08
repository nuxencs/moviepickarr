package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"
)

const (
	radarrInstanceConnected             = "connected"
	radarrInstanceOffline               = "offline"
	radarrInstanceCredentialUnavailable = "credential_unavailable"
)

type radarrClientFactory func(baseURL, apiKey string) (integrationradarr.Client, error)

type cachedRadarrClient struct {
	revision int64
	client   integrationradarr.Client
}

// radarrService is the Admin-facing Radarr module. Handlers depend on this
// typed surface, never on SQLite or Radarr wire resources directly.
type radarrService struct {
	repo      *repository.SqliteRadarrRepository
	secrets   integrationradarr.SecretCodec
	newClient radarrClientFactory
	now       func() time.Time
	publicURL string

	clientsMu sync.Mutex
	clients   map[int64]cachedRadarrClient

	releasesMu sync.Mutex
	releases   map[string]cachedAcquisitionRelease
}

type cachedAcquisitionRelease struct {
	acquisitionID int64
	release       integrationradarr.Release
	expiresAt     time.Time
}

type radarrInstanceDraft struct {
	Name     string
	URL      string
	APIKey   string
	Revision int64
}

type radarrPresetDraft struct {
	Name                string
	InstanceID          int64
	RootFolderPath      string
	QualityProfileID    int
	TagIDs              []int
	MinimumAvailability string
	Mode                string
	Revision            int64
}

func newRadarrService(
	repo *repository.SqliteRadarrRepository,
	secrets integrationradarr.SecretCodec,
	newClient radarrClientFactory,
	publicURL string,
) *radarrService {
	if newClient == nil {
		newClient = func(baseURL, apiKey string) (integrationradarr.Client, error) {
			return integrationradarr.NewHTTPClient(integrationradarr.ClientConfig{
				BaseURL: baseURL,
				APIKey:  apiKey,
				HTTPClient: &http.Client{
					Timeout: 8 * time.Second,
				},
			})
		}
	}
	return &radarrService{
		repo: repo, secrets: secrets, newClient: newClient,
		now: time.Now, publicURL: normalizePublicURL(publicURL),
		clients:  make(map[int64]cachedRadarrClient),
		releases: make(map[string]cachedAcquisitionRelease),
	}
}

func normalizePublicURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}

func (s *radarrService) listInstances(ctx context.Context) ([]repository.RadarrInstance, error) {
	return s.repo.ListInstances(ctx, true)
}

func (s *radarrService) saveInstance(
	ctx context.Context,
	id *int64,
	draft radarrInstanceDraft,
) (repository.RadarrInstance, error) {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.URL = strings.TrimSpace(draft.URL)
	draft.APIKey = strings.TrimSpace(draft.APIKey)
	if draft.Name == "" {
		return repository.RadarrInstance{}, invalidRadarrField("name", "Name is required.")
	}
	if len(draft.Name) > 80 {
		return repository.RadarrInstance{}, invalidRadarrField("name", "Name must be 80 characters or fewer.")
	}
	if draft.URL == "" {
		return repository.RadarrInstance{}, invalidRadarrField("url", "URL is required.")
	}
	normalizedURL, err := normalizeInstanceURL(draft.URL)
	if err != nil {
		return repository.RadarrInstance{}, invalidRadarrField("url", err.Error())
	}

	instances, err := s.repo.ListInstances(ctx, false)
	if err != nil {
		return repository.RadarrInstance{}, err
	}
	for _, instance := range instances {
		if strings.EqualFold(instance.Name, draft.Name) && (id == nil || instance.ID != *id) {
			return repository.RadarrInstance{}, invalidRadarrField("name", "An active instance already uses this name.")
		}
	}

	apiKey := draft.APIKey
	var encrypted []byte
	if id != nil {
		current, getErr := s.repo.GetInstance(ctx, *id)
		if getErr != nil {
			return repository.RadarrInstance{}, getErr
		}
		if current.ArchivedAt != nil {
			return repository.RadarrInstance{}, domain.ErrConflict
		}
		if draft.Revision <= 0 || draft.Revision != current.Revision {
			return repository.RadarrInstance{}, errRadarrStaleRevision
		}
		if apiKey == "" {
			if !sameRadarrInstanceAuthority(current.BaseURL, normalizedURL) {
				return repository.RadarrInstance{}, invalidRadarrField(
					"apiKey",
					"Enter the API key again when the URL scheme or host changes.",
				)
			}
			apiKey, err = s.secrets.Decrypt(current.EncryptedAPIKey)
			if err != nil {
				return repository.RadarrInstance{}, fmt.Errorf("decrypt Radarr API key: %w", err)
			}
			encrypted = current.EncryptedAPIKey
		}
	} else if apiKey == "" {
		return repository.RadarrInstance{}, invalidRadarrField("apiKey", "API key is required.")
	}

	client, err := s.newClient(normalizedURL, apiKey)
	if err != nil {
		return repository.RadarrInstance{}, invalidRadarrField("url", "Enter a valid Radarr URL.")
	}
	if _, err := client.VerifyAndCatalog(ctx); err != nil {
		return repository.RadarrInstance{}, fmt.Errorf("verify Radarr instance: %w", err)
	}
	if encrypted == nil {
		encrypted, err = s.secrets.Encrypt(apiKey)
		if err != nil {
			return repository.RadarrInstance{}, fmt.Errorf("encrypt Radarr API key: %w", err)
		}
	}
	now := s.now().UTC()
	save := repository.RadarrInstanceSave{
		Name: draft.Name, BaseURL: normalizedURL, EncryptedAPIKey: encrypted,
		State: radarrInstanceConnected, CheckedAt: now,
	}
	var saved repository.RadarrInstance
	if id == nil {
		saved, err = s.repo.CreateInstance(ctx, save)
	} else {
		saved, err = s.repo.UpdateInstance(ctx, *id, draft.Revision, save)
	}
	if err != nil {
		return repository.RadarrInstance{}, err
	}
	s.clientsMu.Lock()
	s.clients[saved.ID] = cachedRadarrClient{revision: saved.Revision, client: client}
	s.clientsMu.Unlock()
	return saved, nil
}

func normalizeInstanceURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", errors.New("enter a valid Radarr URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("the URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("the URL cannot contain credentials, a query, or a fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

func sameRadarrInstanceAuthority(left, right string) bool {
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	if leftErr != nil || rightErr != nil || leftURL == nil || rightURL == nil {
		return false
	}
	return strings.EqualFold(leftURL.Scheme, rightURL.Scheme) &&
		strings.EqualFold(leftURL.Host, rightURL.Host)
}

func (s *radarrService) removeInstance(
	ctx context.Context,
	id int64,
) (repository.RadarrRemoveOutcome, error) {
	outcome, err := s.repo.RemoveInstance(ctx, id, s.now().UTC())
	if err != nil {
		return "", err
	}
	s.clientsMu.Lock()
	delete(s.clients, id)
	s.clientsMu.Unlock()
	return outcome, nil
}

func (s *radarrService) instanceCatalog(
	ctx context.Context,
	id int64,
) (integrationradarr.Catalog, error) {
	instance, err := s.repo.GetInstance(ctx, id)
	if err != nil {
		return integrationradarr.Catalog{}, err
	}
	if instance.ArchivedAt != nil {
		return integrationradarr.Catalog{}, domain.ErrConflict
	}
	return s.catalogFor(ctx, instance)
}

func (s *radarrService) catalogFor(
	ctx context.Context,
	instance repository.RadarrInstance,
) (integrationradarr.Catalog, error) {
	client, err := s.clientFor(instance)
	if err != nil {
		_ = s.repo.UpdateInstanceState(ctx, instance.ID, radarrInstanceCredentialUnavailable,
			"Stored API key cannot be decrypted with the current instance key.", s.now().UTC())
		return integrationradarr.Catalog{}, err
	}
	catalog, err := client.VerifyAndCatalog(ctx)
	if err != nil {
		_ = s.repo.UpdateInstanceState(ctx, instance.ID, radarrInstanceOffline,
			"Radarr could not be reached or rejected the request.", s.now().UTC())
		return integrationradarr.Catalog{}, err
	}
	if instance.State != radarrInstanceConnected || instance.StateReason != "" {
		_ = s.repo.UpdateInstanceState(ctx, instance.ID, radarrInstanceConnected, "", s.now().UTC())
	}
	return catalog, nil
}

func (s *radarrService) clientFor(instance repository.RadarrInstance) (integrationradarr.Client, error) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	if cached, ok := s.clients[instance.ID]; ok && cached.revision == instance.Revision {
		return cached.client, nil
	}
	apiKey, err := s.secrets.Decrypt(instance.EncryptedAPIKey)
	if err != nil {
		return nil, err
	}
	client, err := s.newClient(instance.BaseURL, apiKey)
	if err != nil {
		return nil, err
	}
	s.clients[instance.ID] = cachedRadarrClient{revision: instance.Revision, client: client}
	return client, nil
}

func (s *radarrService) listPresets(ctx context.Context) ([]repository.RadarrPreset, error) {
	return s.repo.ListPresets(ctx, true)
}

func (s *radarrService) savePreset(
	ctx context.Context,
	id *int64,
	draft radarrPresetDraft,
) (repository.RadarrPreset, error) {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.RootFolderPath = strings.TrimSpace(draft.RootFolderPath)
	if draft.Name == "" {
		return repository.RadarrPreset{}, invalidRadarrField("name", "Name is required.")
	}
	if draft.InstanceID <= 0 {
		return repository.RadarrPreset{}, invalidRadarrField("instanceId", "Select an instance.")
	}
	if draft.RootFolderPath == "" {
		return repository.RadarrPreset{}, invalidRadarrField("rootFolderPath", "Select a root folder.")
	}
	if draft.QualityProfileID <= 0 {
		return repository.RadarrPreset{}, invalidRadarrField("qualityProfileId", "Select a quality profile.")
	}
	if !validMinimumAvailability(draft.MinimumAvailability) {
		return repository.RadarrPreset{}, invalidRadarrField("minimumAvailability", "Select a minimum availability.")
	}
	if draft.Mode != "manual" && draft.Mode != "automatic" {
		return repository.RadarrPreset{}, invalidRadarrField("mode", "Select an Acquisition mode.")
	}
	if id != nil {
		current, err := s.repo.GetPreset(ctx, *id)
		if err != nil {
			return repository.RadarrPreset{}, err
		}
		if current.ArchivedAt != nil {
			return repository.RadarrPreset{}, domain.ErrConflict
		}
		if draft.Revision <= 0 || draft.Revision != current.Revision {
			return repository.RadarrPreset{}, errRadarrStaleRevision
		}
	}
	presets, err := s.repo.ListPresets(ctx, false)
	if err != nil {
		return repository.RadarrPreset{}, err
	}
	for _, preset := range presets {
		if strings.EqualFold(preset.Name, draft.Name) && (id == nil || preset.ID != *id) {
			return repository.RadarrPreset{}, invalidRadarrField("name", "An active preset already uses this name.")
		}
	}

	instance, err := s.repo.GetInstance(ctx, draft.InstanceID)
	if err != nil {
		return repository.RadarrPreset{}, err
	}
	if instance.ArchivedAt != nil {
		return repository.RadarrPreset{}, domain.ErrConflict
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return repository.RadarrPreset{}, fmt.Errorf("verify Radarr preset: %w", err)
	}
	save, err := validatedPresetSave(draft, catalog, s.now().UTC())
	if err != nil {
		return repository.RadarrPreset{}, err
	}
	if id == nil {
		return s.repo.CreatePreset(ctx, save)
	}
	return s.repo.UpdatePreset(ctx, *id, draft.Revision, save)
}

func validatedPresetSave(
	draft radarrPresetDraft,
	catalog integrationradarr.Catalog,
	validatedAt time.Time,
) (repository.RadarrPresetSave, error) {
	rootIndex := slices.IndexFunc(catalog.RootFolders, func(root integrationradarr.RootFolder) bool {
		return root.Path == draft.RootFolderPath
	})
	if rootIndex < 0 {
		return repository.RadarrPresetSave{}, invalidRadarrField("rootFolderPath", "The root folder no longer exists on this instance.")
	}
	root := catalog.RootFolders[rootIndex]
	if !root.Accessible {
		return repository.RadarrPresetSave{}, invalidRadarrField("rootFolderPath", "The root folder is not accessible to Radarr.")
	}
	profileIndex := slices.IndexFunc(catalog.QualityProfiles, func(profile integrationradarr.QualityProfile) bool {
		return profile.ID == draft.QualityProfileID
	})
	if profileIndex < 0 {
		return repository.RadarrPresetSave{}, invalidRadarrField("qualityProfileId", "The quality profile no longer exists on this instance.")
	}
	profile := catalog.QualityProfiles[profileIndex]
	tags := make([]repository.RadarrTagSnapshot, 0, len(draft.TagIDs))
	seen := make(map[int]struct{}, len(draft.TagIDs))
	for _, id := range draft.TagIDs {
		if id <= 0 {
			return repository.RadarrPresetSave{}, invalidRadarrField("tagIds", "Tag IDs must be positive.")
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		index := slices.IndexFunc(catalog.Tags, func(tag integrationradarr.Tag) bool { return tag.ID == id })
		if index < 0 {
			return repository.RadarrPresetSave{}, invalidRadarrField("tagIds", "A selected tag no longer exists on this instance.")
		}
		tags = append(tags, repository.RadarrTagSnapshot{ID: id, Label: catalog.Tags[index].Label})
	}
	return repository.RadarrPresetSave{
		Name: draft.Name, InstanceID: draft.InstanceID,
		RootFolderID: root.ID, RootFolderPath: root.Path,
		QualityProfileID: profile.ID, QualityProfileName: profile.Name,
		Tags: tags, MinimumAvailability: draft.MinimumAvailability,
		AcquisitionMode: draft.Mode, Valid: true, ValidatedAt: validatedAt,
	}, nil
}

func (s *radarrService) validateStoredPreset(
	ctx context.Context,
	preset repository.RadarrPreset,
) (repository.RadarrPreset, integrationradarr.Catalog, error) {
	instance, err := s.repo.GetInstance(ctx, preset.InstanceID)
	if err != nil {
		return preset, integrationradarr.Catalog{}, err
	}
	if instance.ArchivedAt != nil {
		_ = s.repo.SetPresetValidity(ctx, preset.ID, false, "The target instance is archived.", s.now().UTC())
		return preset, integrationradarr.Catalog{}, domain.ErrConflict
	}
	catalog, err := s.catalogFor(ctx, instance)
	if err != nil {
		return preset, integrationradarr.Catalog{}, err
	}
	draft := radarrPresetDraft{
		Name: preset.Name, InstanceID: preset.InstanceID,
		RootFolderPath: preset.RootFolderPath, QualityProfileID: preset.QualityProfileID,
		MinimumAvailability: preset.MinimumAvailability, Mode: preset.AcquisitionMode,
	}
	for _, tag := range preset.Tags {
		draft.TagIDs = append(draft.TagIDs, tag.ID)
	}
	validated, err := validatedPresetSave(draft, catalog, s.now().UTC())
	if err != nil {
		_ = s.repo.SetPresetValidity(ctx, preset.ID, false, "The preset no longer matches the instance catalog.", s.now().UTC())
		return preset, catalog, err
	}
	if !preset.Valid || preset.ValidationReason != "" {
		_ = s.repo.SetPresetValidity(ctx, preset.ID, true, "", validated.ValidatedAt)
		preset.Valid = true
		preset.ValidationReason = ""
		preset.ValidatedAt = validated.ValidatedAt
	}
	return preset, catalog, nil
}

func (s *radarrService) removePreset(
	ctx context.Context,
	id int64,
) (repository.RadarrRemoveOutcome, error) {
	return s.repo.RemovePreset(ctx, id, s.now().UTC())
}

func validMinimumAvailability(value string) bool {
	switch value {
	case "tba", "announced", "inCinemas", "released":
		return true
	default:
		return false
	}
}

type radarrFieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *radarrFieldError) Error() string { return e.Message }

func invalidRadarrField(field, message string) error {
	return &radarrFieldError{Field: field, Message: message}
}

var errRadarrStaleRevision = errors.New("stale Radarr configuration revision")
