package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	"moviepickarr/internal/repository"
)

const (
	radarrWebhookDispatchInterval = 15 * time.Second
	radarrWebhookClaimLease       = 30 * time.Second
)

var allRadarrActionReasons = []string{
	"preset_required",
	"identity_required",
	"release_required",
	"configuration_invalid",
	"connection_failed",
	"add_failed",
	"no_releases",
	"release_failed",
	"import_failed",
	"monitoring_failed",
}

type radarrWebhookDraft struct {
	Name        string
	Kind        string
	URL         string
	Enabled     bool
	Reasons     []string
	RoleMention string
	Revision    int64
}

type radarrWebhookSender interface {
	Send(context.Context, string, any) error
}

type httpRadarrWebhookSender struct {
	client *http.Client
}

type radarrWebhookHTTPError struct {
	Status int
}

var errRadarrWebhookUnavailable = errors.New("webhook endpoint is unavailable")

func (e *radarrWebhookHTTPError) Error() string {
	return fmt.Sprintf("webhook endpoint returned HTTP %d", e.Status)
}

func (s httpRadarrWebhookSender) Send(ctx context.Context, endpoint string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode webhook payload: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "moviepickarr-radarr-webhook/1")
	response, err := s.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(errRadarrWebhookUnavailable, ctx.Err())
		}
		return errRadarrWebhookUnavailable
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &radarrWebhookHTTPError{Status: response.StatusCode}
	}
	return nil
}

func defaultRadarrWebhookSender() radarrWebhookSender {
	return httpRadarrWebhookSender{client: &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (s *radarrService) listWebhookDestinations(
	ctx context.Context,
) ([]repository.RadarrWebhookDestination, error) {
	return s.repo.ListWebhookDestinations(ctx, true)
}

func (s *radarrService) saveWebhookDestination(
	ctx context.Context,
	id *int64,
	draft radarrWebhookDraft,
) (repository.RadarrWebhookDestination, error) {
	draft.Name = strings.TrimSpace(draft.Name)
	draft.URL = strings.TrimSpace(draft.URL)
	draft.RoleMention = strings.TrimSpace(draft.RoleMention)
	if draft.Name == "" {
		return repository.RadarrWebhookDestination{}, invalidRadarrField("name", "Name is required.")
	}
	if draft.Kind != "generic" && draft.Kind != "discord" {
		return repository.RadarrWebhookDestination{}, invalidRadarrField("format", "Select Generic or Discord.")
	}
	if draft.Kind == "generic" && draft.RoleMention != "" {
		return repository.RadarrWebhookDestination{}, invalidRadarrField("roleMention", "Role mention is available only for Discord.")
	}
	if draft.Kind == "discord" && !validDiscordRoleMention(draft.RoleMention) {
		return repository.RadarrWebhookDestination{}, invalidRadarrField("roleMention", "Enter a Discord role ID or leave it empty.")
	}
	reasons, err := validateRadarrReasons(draft.Reasons)
	if err != nil {
		return repository.RadarrWebhookDestination{}, err
	}
	draft.Reasons = reasons

	destinations, err := s.repo.ListWebhookDestinations(ctx, false)
	if err != nil {
		return repository.RadarrWebhookDestination{}, err
	}
	for _, destination := range destinations {
		if strings.EqualFold(destination.Name, draft.Name) && (id == nil || destination.ID != *id) {
			return repository.RadarrWebhookDestination{}, invalidRadarrField("name", "An active webhook destination already uses this name.")
		}
	}

	var current repository.RadarrWebhookDestination
	var encryptedURL []byte
	var verifiedAt *time.Time
	if id == nil {
		if draft.URL == "" {
			return repository.RadarrWebhookDestination{}, invalidRadarrField("url", "Webhook URL is required.")
		}
		if _, err := validateWebhookURL(draft.URL); err != nil {
			return repository.RadarrWebhookDestination{}, invalidRadarrField("url", err.Error())
		}
		encryptedURL, err = s.secrets.Encrypt(draft.URL)
		if err != nil {
			return repository.RadarrWebhookDestination{}, fmt.Errorf("encrypt Radarr webhook URL: %w", err)
		}
		// Verification is durable and destination-addressed. A new draft always
		// saves disabled, even if a transient draft test succeeded.
		draft.Enabled = false
	} else {
		current, err = s.repo.GetWebhookDestination(ctx, *id)
		if err != nil {
			return repository.RadarrWebhookDestination{}, err
		}
		if current.ArchivedAt != nil {
			return repository.RadarrWebhookDestination{}, domain.ErrConflict
		}
		if draft.Revision <= 0 || draft.Revision != current.Revision {
			return repository.RadarrWebhookDestination{}, errRadarrStaleRevision
		}
		encryptedURL = current.EncryptedURL
		verifiedAt = current.VerifiedAt
		if draft.Kind != current.Kind || normalizedDiscordRoleID(draft.RoleMention) != current.DiscordRoleMention {
			verifiedAt = nil
			draft.Enabled = false
		}
		if draft.URL != "" {
			if _, err := validateWebhookURL(draft.URL); err != nil {
				return repository.RadarrWebhookDestination{}, invalidRadarrField("url", err.Error())
			}
			currentURL, decryptErr := s.secrets.Decrypt(current.EncryptedURL)
			if decryptErr != nil && !errors.Is(decryptErr, integration.ErrCredentialUnavailable) {
				return repository.RadarrWebhookDestination{}, decryptErr
			}
			if decryptErr != nil || draft.URL != currentURL {
				encryptedURL, err = s.secrets.Encrypt(draft.URL)
				if err != nil {
					return repository.RadarrWebhookDestination{}, fmt.Errorf("encrypt Radarr webhook URL: %w", err)
				}
				verifiedAt = nil
				draft.Enabled = false
			}
		}
	}
	if draft.Enabled && verifiedAt == nil {
		return repository.RadarrWebhookDestination{}, invalidRadarrField("enabled", "Send a successful test before enabling this destination.")
	}
	save := repository.RadarrWebhookDestinationSave{
		Name: draft.Name, Kind: draft.Kind, EncryptedURL: encryptedURL,
		ReasonFilters: draft.Reasons, DiscordRoleMention: normalizedDiscordRoleID(draft.RoleMention),
		Enabled: draft.Enabled, VerifiedAt: verifiedAt,
	}
	if id == nil {
		return s.repo.CreateWebhookDestination(ctx, save)
	}
	return s.repo.UpdateWebhookDestination(ctx, *id, draft.Revision, save)
}

func (s *radarrService) testWebhookDestination(
	ctx context.Context,
	id int64,
) (repository.RadarrWebhookDestination, error) {
	destination, err := s.repo.GetWebhookDestination(ctx, id)
	if err != nil {
		return repository.RadarrWebhookDestination{}, err
	}
	if destination.ArchivedAt != nil {
		return repository.RadarrWebhookDestination{}, domain.ErrConflict
	}
	endpoint, err := s.secrets.Decrypt(destination.EncryptedURL)
	if err != nil {
		return repository.RadarrWebhookDestination{}, err
	}
	payload := renderWebhookTest(destination.Kind, destination.Name, destination.DiscordRoleMention)
	if err := defaultRadarrWebhookSender().Send(ctx, endpoint, payload); err != nil {
		return repository.RadarrWebhookDestination{}, err
	}
	return s.repo.VerifyWebhookDestination(ctx, id, destination.Revision, s.now().UTC())
}

func (s *radarrService) testWebhookDraft(ctx context.Context, draft radarrWebhookDraft) error {
	draft.URL = strings.TrimSpace(draft.URL)
	if draft.Kind != "generic" && draft.Kind != "discord" {
		return invalidRadarrField("format", "Select Generic or Discord.")
	}
	if _, err := validateWebhookURL(draft.URL); err != nil {
		return invalidRadarrField("url", err.Error())
	}
	if draft.Kind == "discord" && !validDiscordRoleMention(strings.TrimSpace(draft.RoleMention)) {
		return invalidRadarrField("roleMention", "Enter a Discord role ID or leave it empty.")
	}
	if _, err := validateRadarrReasons(draft.Reasons); err != nil {
		return err
	}
	payload := renderWebhookTest(draft.Kind, strings.TrimSpace(draft.Name), normalizedDiscordRoleID(draft.RoleMention))
	return defaultRadarrWebhookSender().Send(ctx, draft.URL, payload)
}

func (s *radarrService) archiveWebhookDestination(ctx context.Context, id int64) error {
	return s.repo.ArchiveWebhookDestination(ctx, id, s.now().UTC())
}

func (s *radarrService) deliverDueWebhooks(
	ctx context.Context,
	sender radarrWebhookSender,
	limit int,
) (int, error) {
	if sender == nil {
		sender = defaultRadarrWebhookSender()
	}
	now := s.now().UTC()
	if _, err := s.repo.RecoverExpiredWebhookDeliveryClaims(ctx, now); err != nil {
		return 0, err
	}
	deliveries, err := s.repo.DueWebhookDeliveries(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	var joined error
	for _, selected := range deliveries {
		claimedAt := s.now().UTC()
		delivery, claimed, claimErr := s.repo.ClaimWebhookDeliveryForSend(
			ctx, selected.ID, claimedAt, claimedAt.Add(radarrWebhookClaimLease),
		)
		if claimErr != nil {
			joined = errors.Join(joined, claimErr)
			continue
		}
		if !claimed {
			continue
		}
		endpoint, decryptErr := s.secrets.Decrypt(delivery.EncryptedURL)
		if decryptErr != nil {
			if err := s.repo.MarkWebhookDeliveryFailed(ctx, delivery.ID, delivery.ClaimVersion,
				"Stored webhook URL cannot be decrypted.", s.now().UTC(), true, s.now().UTC()); err != nil {
				joined = errors.Join(joined, err)
			}
			processed++
			continue
		}
		payload := s.renderAcquisitionWebhook(delivery)
		deliveryErr := sender.Send(ctx, endpoint, payload)
		if deliveryErr == nil {
			if err := s.repo.MarkWebhookDelivered(
				ctx, delivery.ID, delivery.ClaimVersion, s.now().UTC(),
			); err != nil {
				joined = errors.Join(joined, err)
			}
			processed++
			continue
		}
		attempt := delivery.AttemptCount
		terminal := attempt >= repository.RadarrWebhookMaxAttempts
		next := s.now().UTC().Add(radarrWebhookBackoff(attempt))
		if err := s.repo.MarkWebhookDeliveryFailed(
			ctx, delivery.ID, delivery.ClaimVersion,
			sanitizeWebhookError(deliveryErr), next, terminal, s.now().UTC(),
		); err != nil {
			joined = errors.Join(joined, err)
		}
		processed++
	}
	return processed, joined
}

func (s *radarrService) renderAcquisitionWebhook(delivery repository.RadarrWebhookDelivery) any {
	actionURL := ""
	if s.publicURL != "" {
		actionURL = fmt.Sprintf("%s/admin/integrations/radarr/acquisitions/%d", s.publicURL, delivery.AcquisitionID)
	}
	if delivery.Kind == "discord" {
		embed := map[string]any{
			"title":       delivery.MovieTitle + " needs attention",
			"description": radarrReasonLabel(delivery.Reason),
			"color":       0xD99A35,
			"fields": []map[string]any{
				{"name": "Reason", "value": radarrReasonLabel(delivery.Reason), "inline": true},
			},
		}
		if delivery.TargetLabel != "" {
			embed["fields"] = append(embed["fields"].([]map[string]any),
				map[string]any{"name": "Target", "value": delivery.TargetLabel, "inline": true})
		}
		if actionURL != "" {
			embed["url"] = actionURL
		}
		payload := map[string]any{"embeds": []any{embed}}
		if delivery.DiscordRoleMention != "" {
			payload["content"] = "<@&" + delivery.DiscordRoleMention + ">"
			payload["allowed_mentions"] = map[string]any{"roles": []string{delivery.DiscordRoleMention}}
		}
		return payload
	}
	return map[string]any{
		"event": "acquisition.action_required",
		"data": map[string]any{
			"deliveryId":    delivery.ID,
			"acquisitionId": delivery.AcquisitionID,
			"actionVersion": delivery.ActionVersion,
			"movieTitle":    delivery.MovieTitle,
			"reason":        delivery.Reason,
			"target":        emptyToNil(delivery.TargetLabel),
			"adminUrl":      emptyToNil(actionURL),
		},
	}
}

func renderWebhookTest(kind, name, roleID string) any {
	if name == "" {
		name = "Webhook destination"
	}
	if kind == "discord" {
		payload := map[string]any{
			"embeds": []any{map[string]any{
				"title":       "moviepickarr webhook test",
				"description": name + " is connected. This is a test, not an Acquisition event.",
				"color":       0x739072,
			}},
		}
		if roleID != "" {
			payload["content"] = "<@&" + roleID + ">"
			payload["allowed_mentions"] = map[string]any{"roles": []string{roleID}}
		}
		return payload
	}
	return map[string]any{
		"test":        true,
		"message":     "moviepickarr webhook test",
		"destination": name,
	}
}

func validateWebhookURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" {
		return nil, errors.New("enter a valid webhook URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("the webhook URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("the webhook URL cannot contain credentials or a fragment")
	}
	return parsed, nil
}

var discordRolePattern = regexp.MustCompile(`^(?:([0-9]{5,25})|<@&([0-9]{5,25})>)$`)

func validDiscordRoleMention(value string) bool {
	return value == "" || discordRolePattern.MatchString(value)
}

func normalizedDiscordRoleID(value string) string {
	match := discordRolePattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 3 {
		return ""
	}
	if match[1] != "" {
		return match[1]
	}
	return match[2]
}

func validateRadarrReasons(reasons []string) ([]string, error) {
	result := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if !slices.Contains(allRadarrActionReasons, reason) {
			return nil, invalidRadarrField("reasons", "Select only supported actionable reasons.")
		}
		if !slices.Contains(result, reason) {
			result = append(result, reason)
		}
	}
	if len(result) == 0 {
		return nil, invalidRadarrField("reasons", "Select at least one actionable reason.")
	}
	return result, nil
}

func radarrWebhookBackoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 30 * time.Minute
	default:
		return 2 * time.Hour
	}
}

func sanitizeWebhookError(err error) string {
	var httpErr *radarrWebhookHTTPError
	if errors.As(err, &httpErr) {
		return fmt.Sprintf("Webhook endpoint returned HTTP %d.", httpErr.Status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Webhook request timed out."
	}
	return "Webhook endpoint could not be reached."
}

func radarrReasonLabel(reason string) string {
	labels := map[string]string{
		"preset_required":       "Select an Acquisition preset",
		"identity_required":     "Select the correct movie identity",
		"release_required":      "Select a release",
		"configuration_invalid": "Repair the target configuration",
		"connection_failed":     "Restore the Radarr connection",
		"add_failed":            "Add or recreate the movie",
		"no_releases":           "No matched releases were found",
		"release_failed":        "The release or download failed",
		"import_failed":         "The download could not be imported",
		"monitoring_failed":     "Enable monitoring in Radarr",
	}
	if label := labels[reason]; label != "" {
		return label
	}
	return "Admin action is required"
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type radarrWebhookWorker struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func newRadarrWebhookWorker(
	parent context.Context,
	service *radarrService,
	onErr func(error),
) *radarrWebhookWorker {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	worker := &radarrWebhookWorker{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(worker.done)
		if _, err := service.repo.PruneWebhookDeliveries(ctx, service.now().UTC()); err != nil && ctx.Err() == nil && onErr != nil {
			onErr(err)
		}
		if _, err := service.deliverDueWebhooks(ctx, nil, 50); err != nil && ctx.Err() == nil && onErr != nil {
			onErr(err)
		}
		dispatch := time.NewTicker(radarrWebhookDispatchInterval)
		retention := time.NewTicker(24 * time.Hour)
		defer dispatch.Stop()
		defer retention.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-dispatch.C:
				if _, err := service.deliverDueWebhooks(ctx, nil, 50); err != nil && ctx.Err() == nil && onErr != nil {
					onErr(err)
				}
			case <-retention.C:
				if _, err := service.repo.PruneWebhookDeliveries(ctx, service.now().UTC()); err != nil && ctx.Err() == nil && onErr != nil {
					onErr(err)
				}
			}
		}
	}()
	return worker
}

func (w *radarrWebhookWorker) Close() {
	if w == nil {
		return
	}
	w.once.Do(w.cancel)
	<-w.done
}
