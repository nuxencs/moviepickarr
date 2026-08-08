package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

type radarrAttentionResponse struct {
	Count int `json:"count"`
}

type radarrAbandonmentReviewResponse struct {
	Acquisition radarrAcquisitionResponse `json:"acquisition"`
	Activity    string                    `json:"activity"`
}

type radarrIdentityResponse struct {
	TMDBID int    `json:"tmdbId,omitempty"`
	IMDbID string `json:"imdbId,omitempty"`
	Title  string `json:"title,omitempty"`
	Year   int    `json:"year,omitempty"`
	Source string `json:"source,omitempty"`
}

type radarrTagResponse struct {
	ID    int    `json:"id"`
	Label string `json:"label,omitempty"`
}

type radarrTargetResponse struct {
	PresetID            int64               `json:"presetId,omitempty"`
	PresetName          string              `json:"presetName,omitempty"`
	InstanceID          int64               `json:"instanceId,omitempty"`
	InstanceName        string              `json:"instanceName,omitempty"`
	RootFolderPath      string              `json:"rootFolderPath,omitempty"`
	QualityProfileID    int                 `json:"qualityProfileId,omitempty"`
	QualityProfileName  string              `json:"qualityProfileName,omitempty"`
	Tags                []radarrTagResponse `json:"tags,omitempty"`
	MinimumAvailability string              `json:"minimumAvailability,omitempty"`
	Mode                string              `json:"mode,omitempty"`
}

type radarrEffectiveConfigResponse struct {
	RootFolderPath      string              `json:"rootFolderPath,omitempty"`
	QualityProfileID    int                 `json:"qualityProfileId,omitempty"`
	QualityProfileName  string              `json:"qualityProfileName,omitempty"`
	Tags                []radarrTagResponse `json:"tags,omitempty"`
	MinimumAvailability string              `json:"minimumAvailability,omitempty"`
	Monitored           bool                `json:"monitored"`
}

type radarrReleaseSummaryResponse struct {
	Title      string `json:"title,omitempty"`
	Quality    string `json:"quality,omitempty"`
	SelectedAt string `json:"selectedAt,omitempty"`
}

type radarrAcquisitionMilestonesResponse struct {
	CreatedAt        string `json:"createdAt,omitempty"`
	RevealedAt       string `json:"revealedAt,omitempty"`
	TargetSelectedAt string `json:"targetSelectedAt,omitempty"`
	AddedAt          string `json:"addedAt,omitempty"`
	GrabbedAt        string `json:"grabbedAt,omitempty"`
	DownloadedAt     string `json:"downloadedAt,omitempty"`
	AbandonedAt      string `json:"abandonedAt,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

type radarrAcquisitionResponse struct {
	ID                    int64                               `json:"id"`
	MovieID               int                                 `json:"movieId"`
	Title                 string                              `json:"title"`
	Year                  int                                 `json:"year,omitempty"`
	Status                string                              `json:"status"`
	MutationState         string                              `json:"mutationState"`
	ActionReason          string                              `json:"actionReason,omitempty"`
	ActionMessage         string                              `json:"actionMessage,omitempty"`
	Identity              *radarrIdentityResponse             `json:"identity,omitempty"`
	Preset                *radarrTargetResponse               `json:"preset,omitempty"`
	Target                *radarrTargetResponse               `json:"target,omitempty"`
	TargetLocked          bool                                `json:"targetLocked"`
	TargetPreviewedAt     string                              `json:"targetPreviewedAt,omitempty"`
	TargetPreviewExisting bool                                `json:"targetPreviewExisting"`
	PreviewReady          bool                                `json:"previewReady"`
	RadarrMovieID         int                                 `json:"radarrMovieId,omitempty"`
	Existing              bool                                `json:"existing"`
	ExistingMovie         bool                                `json:"existingMovie"`
	AdoptedExisting       bool                                `json:"adoptedExisting"`
	EffectiveConfig       *radarrEffectiveConfigResponse      `json:"effectiveConfig,omitempty"`
	ActiveQueue           bool                                `json:"activeQueue"`
	LatestRelease         *radarrReleaseSummaryResponse       `json:"latestRelease,omitempty"`
	ManualAttemptCount    int                                 `json:"manualAttemptCount"`
	LatestFailure         string                              `json:"latestFailure,omitempty"`
	AbandonmentReason     string                              `json:"abandonmentReason,omitempty"`
	Milestones            radarrAcquisitionMilestonesResponse `json:"milestones"`
	CreatedAt             string                              `json:"createdAt"`
	UpdatedAt             string                              `json:"updatedAt"`
}

type radarrInstanceResponse struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	URL              string `json:"url,omitempty"`
	State            string `json:"state,omitempty"`
	Reason           string `json:"reason,omitempty"`
	APIKeyConfigured bool   `json:"apiKeyConfigured"`
	LastTestedAt     string `json:"lastTestedAt,omitempty"`
	ArchivedAt       string `json:"archivedAt,omitempty"`
	Revision         int64  `json:"revision"`
	Used             bool   `json:"used"`
}

type radarrRootFolderResponse struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	FreeSpace  int64  `json:"freeSpace,omitempty"`
	Accessible bool   `json:"accessible"`
}

type radarrQualityProfileResponse struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type radarrInstanceOptionsResponse struct {
	RootFolders     []radarrRootFolderResponse     `json:"rootFolders"`
	QualityProfiles []radarrQualityProfileResponse `json:"qualityProfiles"`
	Tags            []radarrTagResponse            `json:"tags"`
}

type radarrPresetResponse struct {
	ID                  int64               `json:"id"`
	Name                string              `json:"name"`
	InstanceID          int64               `json:"instanceId"`
	InstanceName        string              `json:"instanceName,omitempty"`
	RootFolderPath      string              `json:"rootFolderPath"`
	QualityProfileID    int                 `json:"qualityProfileId"`
	QualityProfileName  string              `json:"qualityProfileName,omitempty"`
	TagIDs              []int               `json:"tagIds"`
	Tags                []radarrTagResponse `json:"tags"`
	MinimumAvailability string              `json:"minimumAvailability"`
	Mode                string              `json:"mode"`
	Valid               bool                `json:"valid"`
	InvalidReason       string              `json:"invalidReason,omitempty"`
	ArchivedAt          string              `json:"archivedAt,omitempty"`
	Revision            int64               `json:"revision"`
	Used                bool                `json:"used"`
}

type radarrRemoveResponse struct {
	Outcome repository.RadarrRemoveOutcome `json:"outcome"`
}

type radarrWebhookResponse struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Format       string   `json:"format"`
	Enabled      bool     `json:"enabled"`
	Verified     bool     `json:"verified"`
	Reasons      []string `json:"reasons"`
	RoleMention  string   `json:"roleMention,omitempty"`
	Health       string   `json:"health,omitempty"`
	HealthReason string   `json:"healthReason,omitempty"`
	LastTestedAt string   `json:"lastTestedAt,omitempty"`
	ArchivedAt   string   `json:"archivedAt,omitempty"`
	Revision     int64    `json:"revision"`
}

type radarrIdentityResultResponse struct {
	TMDBID int    `json:"tmdbId"`
	IMDbID string `json:"imdbId,omitempty"`
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
}

type radarrReleaseResponse struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Quality           string   `json:"quality,omitempty"`
	Size              int64    `json:"size,omitempty"`
	AgeHours          float64  `json:"ageHours,omitempty"`
	Peers             *int     `json:"peers,omitempty"`
	Protocol          string   `json:"protocol,omitempty"`
	Indexer           string   `json:"indexer,omitempty"`
	CustomFormats     []string `json:"customFormats,omitempty"`
	CustomFormatScore int      `json:"customFormatScore"`
	Approved          bool     `json:"approved"`
	Rejected          bool     `json:"rejected"`
	Rejections        []string `json:"rejections"`
	Mapped            bool     `json:"mapped"`
	GrabAllowed       bool     `json:"grabAllowed"`
}

type radarrInstanceRequest struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	APIKey   string `json:"apiKey"`
	Revision int64  `json:"revision"`
}

type radarrPresetRequest struct {
	Name                string `json:"name"`
	InstanceID          int64  `json:"instanceId"`
	RootFolderPath      string `json:"rootFolderPath"`
	QualityProfileID    int    `json:"qualityProfileId"`
	TagIDs              []int  `json:"tagIds"`
	MinimumAvailability string `json:"minimumAvailability"`
	Mode                string `json:"mode"`
	Revision            int64  `json:"revision"`
}

type radarrWebhookRequest struct {
	Name        string   `json:"name"`
	Format      string   `json:"format"`
	URL         string   `json:"url"`
	Enabled     bool     `json:"enabled"`
	Reasons     []string `json:"reasons"`
	RoleMention string   `json:"roleMention"`
	Revision    int64    `json:"revision"`
}

type radarrIssueResponse struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type radarrProblemResponse struct {
	problemDetails
	Issues []radarrIssueResponse `json:"issues,omitempty"`
}

func (h *handler) requireRadarrAdmin(c *fiber.Ctx) (*radarrService, error) {
	if ok, err := h.requireAdmin(c); !ok {
		return nil, err
	}
	if h.radarr == nil {
		return nil, writeProblem(c, fiber.StatusServiceUnavailable, "integration_unavailable", "Radarr is unavailable")
	}
	return h.radarr, nil
}

func (h *handler) handleGetRadarrAttention(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	count, err := service.attentionCount(c.UserContext())
	if err != nil {
		return h.writeRadarrError(c, err, "counting Radarr acquisitions failed")
	}
	return c.Status(fiber.StatusOK).JSON(radarrAttentionResponse{Count: count})
}

func (h *handler) handleListRadarrAcquisitions(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	acquisitions, err := service.listAcquisitions(c.UserContext(), c.Query("query"))
	if err != nil {
		return h.writeRadarrError(c, err, "listing Radarr acquisitions failed")
	}
	responses := make([]radarrAcquisitionResponse, 0, len(acquisitions))
	for _, acquisition := range acquisitions {
		responses = append(responses, toRadarrAcquisitionResponse(acquisition))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleGetRadarrAcquisition(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	acquisition, err := service.acquisition(c.UserContext(), id)
	if err != nil {
		return h.writeRadarrError(c, err, "reading Radarr acquisition failed")
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrAcquisitionResponse(acquisition))
}

func (h *handler) handleSelectRadarrPreset(c *fiber.Ctx) error {
	service, acquisitionID, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	var request struct {
		PresetID int64 `json:"presetId"`
	}
	if err := c.BodyParser(&request); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	if request.PresetID <= 0 {
		return h.writeRadarrError(c, invalidRadarrField("presetId", "Select a preset."), "selecting Radarr preset failed")
	}
	acquisition, err := service.selectPreset(c.UserContext(), acquisitionID, request.PresetID, actorMemberID(c))
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "selecting Radarr preset failed")
}

func (h *handler) handleConfirmRadarrTarget(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	acquisition, err := service.confirmAcquisitionTarget(c.UserContext(), id, actorMemberID(c))
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "confirming Radarr target failed")
}

func (h *handler) handleSearchRadarrIdentity(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	var request struct {
		Query string `json:"query"`
	}
	if err := c.BodyParser(&request); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	candidates, err := service.searchAcquisitionIdentity(c.UserContext(), id, request.Query)
	if err != nil {
		return h.writeRadarrError(c, err, "searching Radarr movie identities failed")
	}
	responses := make([]radarrIdentityResultResponse, 0, len(candidates))
	for _, candidate := range candidates {
		responses = append(responses, radarrIdentityResultResponse{
			TMDBID: candidate.TMDBID, IMDbID: candidate.IMDbID,
			Title: candidate.Title, Year: candidate.Year,
		})
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleSelectRadarrIdentity(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	var request struct {
		TMDBID int `json:"tmdbId"`
	}
	if err := c.BodyParser(&request); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	acquisition, err := service.selectAcquisitionIdentity(
		c.UserContext(), id, request.TMDBID, actorMemberID(c),
	)
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "selecting Radarr movie identity failed")
}

func (h *handler) handleSearchRadarrReleases(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	releases, err := service.searchReleases(c.UserContext(), id)
	if err != nil {
		return h.writeRadarrError(c, err, "searching Radarr releases failed")
	}
	now := time.Now().UTC()
	responses := make([]radarrReleaseResponse, 0, len(releases))
	for _, release := range releases {
		responses = append(responses, toRadarrReleaseResponse(release, now))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleGrabRadarrRelease(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	var request struct {
		Override bool `json:"override"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&request); err != nil {
			return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
		}
	}
	acquisition, err := service.grabRelease(
		c.UserContext(), id, c.Params("resultId"), request.Override, actorMemberID(c),
	)
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "grabbing Radarr release failed")
}

func (h *handler) handleRetryRadarrAcquisition(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	acquisition, err := service.retryAcquisition(c.UserContext(), id, actorMemberID(c))
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "retrying Radarr acquisition failed")
}

func (h *handler) handleReviewRadarrAbandonment(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	review, err := service.reviewAbandonment(c.UserContext(), id)
	if err != nil {
		return h.writeRadarrError(c, err, "reviewing Radarr abandonment failed")
	}
	return c.Status(fiber.StatusOK).JSON(radarrAbandonmentReviewResponse{
		Acquisition: toRadarrAcquisitionResponse(review.Acquisition),
		Activity:    review.Activity,
	})
}

func (h *handler) handleAbandonRadarrAcquisition(c *fiber.Ctx) error {
	service, id, err := h.radarrActionContext(c)
	if service == nil || err != nil {
		return err
	}
	var request struct {
		Reason               string `json:"reason"`
		AcknowledgedActivity string `json:"acknowledgedActivity"`
	}
	if err := c.BodyParser(&request); err != nil {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	acquisition, err := service.abandonAcquisition(
		c.UserContext(), id, actorMemberID(c), request.Reason, request.AcknowledgedActivity,
	)
	return h.writeRadarrAcquisitionResult(c, acquisition, err, "abandoning Radarr acquisition failed")
}

func (h *handler) handleListRadarrInstances(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	instances, err := service.listInstances(c.UserContext())
	if err != nil {
		return h.writeRadarrError(c, err, "listing Radarr instances failed")
	}
	responses := make([]radarrInstanceResponse, 0, len(instances))
	for _, instance := range instances {
		responses = append(responses, toRadarrInstanceResponse(instance))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleCreateRadarrInstance(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	request, err := parseRadarrInstanceRequest(c)
	if err != nil {
		return err
	}
	instance, saveErr := service.saveInstance(c.UserContext(), nil, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "creating Radarr instance failed")
	}
	return c.Status(fiber.StatusCreated).JSON(toRadarrInstanceResponse(instance))
}

func (h *handler) handleUpdateRadarrInstance(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	request, err := parseRadarrInstanceRequest(c)
	if err != nil {
		return err
	}
	instance, saveErr := service.saveInstance(c.UserContext(), &id, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "updating Radarr instance failed")
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrInstanceResponse(instance))
}

func (h *handler) handleRemoveRadarrInstance(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	outcome, removeErr := service.removeInstance(c.UserContext(), id)
	if removeErr != nil {
		return h.writeRadarrError(c, removeErr, "removing Radarr instance failed")
	}
	return c.Status(fiber.StatusOK).JSON(radarrRemoveResponse{Outcome: outcome})
}

func (h *handler) handleGetRadarrInstanceOptions(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	catalog, err := service.instanceCatalog(c.UserContext(), id)
	if err != nil {
		return h.writeRadarrError(c, err, "reading Radarr instance options failed")
	}
	response := radarrInstanceOptionsResponse{
		RootFolders:     make([]radarrRootFolderResponse, 0, len(catalog.RootFolders)),
		QualityProfiles: make([]radarrQualityProfileResponse, 0, len(catalog.QualityProfiles)),
		Tags:            make([]radarrTagResponse, 0, len(catalog.Tags)),
	}
	for _, root := range catalog.RootFolders {
		response.RootFolders = append(response.RootFolders, radarrRootFolderResponse{
			ID: root.ID, Path: root.Path, FreeSpace: root.FreeSpace, Accessible: root.Accessible,
		})
	}
	for _, profile := range catalog.QualityProfiles {
		response.QualityProfiles = append(response.QualityProfiles, radarrQualityProfileResponse{ID: profile.ID, Name: profile.Name})
	}
	for _, tag := range catalog.Tags {
		response.Tags = append(response.Tags, radarrTagResponse{ID: tag.ID, Label: tag.Label})
	}
	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) handleListRadarrPresets(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	presets, err := service.listPresets(c.UserContext())
	if err != nil {
		return h.writeRadarrError(c, err, "listing Radarr presets failed")
	}
	responses := make([]radarrPresetResponse, 0, len(presets))
	for _, preset := range presets {
		responses = append(responses, toRadarrPresetResponse(preset))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleCreateRadarrPreset(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	request, err := parseRadarrPresetRequest(c)
	if err != nil {
		return err
	}
	preset, saveErr := service.savePreset(c.UserContext(), nil, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "creating Radarr preset failed")
	}
	return c.Status(fiber.StatusCreated).JSON(toRadarrPresetResponse(preset))
}

func (h *handler) handleUpdateRadarrPreset(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	request, err := parseRadarrPresetRequest(c)
	if err != nil {
		return err
	}
	preset, saveErr := service.savePreset(c.UserContext(), &id, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "updating Radarr preset failed")
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrPresetResponse(preset))
}

func (h *handler) handleRemoveRadarrPreset(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	outcome, removeErr := service.removePreset(c.UserContext(), id)
	if removeErr != nil {
		return h.writeRadarrError(c, removeErr, "removing Radarr preset failed")
	}
	return c.Status(fiber.StatusOK).JSON(radarrRemoveResponse{Outcome: outcome})
}

func (h *handler) handleListRadarrWebhooks(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	destinations, err := service.listWebhookDestinations(c.UserContext())
	if err != nil {
		return h.writeRadarrError(c, err, "listing Radarr webhooks failed")
	}
	responses := make([]radarrWebhookResponse, 0, len(destinations))
	for _, destination := range destinations {
		responses = append(responses, toRadarrWebhookResponse(destination))
	}
	return c.Status(fiber.StatusOK).JSON(responses)
}

func (h *handler) handleCreateRadarrWebhook(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	request, err := parseRadarrWebhookRequest(c)
	if err != nil {
		return err
	}
	destination, saveErr := service.saveWebhookDestination(c.UserContext(), nil, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "creating Radarr webhook failed")
	}
	return c.Status(fiber.StatusCreated).JSON(toRadarrWebhookResponse(destination))
}

func (h *handler) handleUpdateRadarrWebhook(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	request, err := parseRadarrWebhookRequest(c)
	if err != nil {
		return err
	}
	destination, saveErr := service.saveWebhookDestination(c.UserContext(), &id, request)
	if saveErr != nil {
		return h.writeRadarrError(c, saveErr, "updating Radarr webhook failed")
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrWebhookResponse(destination))
}

func (h *handler) handleArchiveRadarrWebhook(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	if err := service.archiveWebhookDestination(c.UserContext(), id); err != nil {
		return h.writeRadarrError(c, err, "archiving Radarr webhook failed")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleTestRadarrWebhook(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return writeError(c, err)
	}
	destination, err := service.testWebhookDestination(c.UserContext(), id)
	if err != nil {
		return h.writeRadarrError(c, err, "testing Radarr webhook failed")
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrWebhookResponse(destination))
}

func (h *handler) handleTestRadarrWebhookDraft(c *fiber.Ctx) error {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return err
	}
	request, err := parseRadarrWebhookRequest(c)
	if err != nil {
		return err
	}
	if err := service.testWebhookDraft(c.UserContext(), request); err != nil {
		return h.writeRadarrError(c, err, "testing Radarr webhook draft failed")
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{"verified": true})
}

func (h *handler) radarrActionContext(c *fiber.Ctx) (*radarrService, int64, error) {
	service, err := h.requireRadarrAdmin(c)
	if service == nil || err != nil {
		return nil, 0, err
	}
	id, err := radarrPathID(c, "id")
	if err != nil {
		return nil, 0, writeError(c, err)
	}
	return service, id, nil
}

func (h *handler) writeRadarrAcquisitionResult(
	c *fiber.Ctx,
	acquisition repository.RadarrAcquisition,
	err error,
	logMessage string,
) error {
	if err != nil {
		return h.writeRadarrError(c, err, logMessage)
	}
	return c.Status(fiber.StatusOK).JSON(toRadarrAcquisitionResponse(acquisition))
}

func (h *handler) writeRadarrError(c *fiber.Ctx, err error, logMessage string) error {
	var fieldError *radarrFieldError
	var webhookHTTPError *radarrWebhookHTTPError
	switch {
	case errors.As(err, &fieldError):
		return writeRadarrProblem(c, fiber.StatusUnprocessableEntity, "validation_failed", "Radarr settings are invalid", fieldError)
	case errors.Is(err, errRadarrStaleRevision), errors.Is(err, integration.ErrStaleRevision):
		return writeProblem(c, fiber.StatusConflict, "stale_revision", "another admin changed these settings")
	case errors.Is(err, integration.ErrCredentialUnavailable):
		return writeProblem(c, fiber.StatusConflict, "credential_unavailable", "replace the stored Radarr credential before retrying")
	case errors.Is(err, integrationradarr.ErrReleaseExpired):
		return writeProblem(c, fiber.StatusConflict, "release_expired", "search again before selecting this release")
	case errors.Is(err, integrationradarr.ErrRejectedRelease):
		return writeProblem(c, fiber.StatusConflict, "override_required", "this release requires an explicit rejection override")
	case errors.Is(err, integrationradarr.ErrAuthentication):
		return writeProblem(c, fiber.StatusUnprocessableEntity, "authentication_failed", "Radarr rejected the API key")
	case errors.Is(err, integrationradarr.ErrValidation), errors.Is(err, integrationradarr.ErrInvalidInput):
		return writeProblem(c, fiber.StatusUnprocessableEntity, "radarr_rejected", "Radarr rejected the request")
	case errors.Is(err, integrationradarr.ErrConflict):
		return writeProblem(c, fiber.StatusConflict, "radarr_conflict", "Radarr reports conflicting state")
	case errors.Is(err, errRadarrWebhookUnavailable), errors.As(err, &webhookHTTPError):
		return writeProblem(c, fiber.StatusUnprocessableEntity, "webhook_test_failed", "the webhook endpoint rejected or did not receive the test")
	case errors.Is(err, integrationradarr.ErrTransient), errors.Is(err, context.DeadlineExceeded):
		return writeProblem(c, fiber.StatusServiceUnavailable, "radarr_unavailable", "Radarr could not be reached")
	case errors.Is(err, integrationradarr.ErrRemote), errors.Is(err, integrationradarr.ErrInvalidResponse), errors.Is(err, integrationradarr.ErrResponseTooLarge):
		return writeProblem(c, fiber.StatusBadGateway, "radarr_error", "Radarr returned an unusable response")
	case errors.Is(err, domain.ErrInvalidInput), errors.Is(err, domain.ErrForbidden),
		errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrConflict):
		return writeError(c, err)
	default:
		return h.writeInternal(c, err, logMessage)
	}
}

func writeRadarrProblem(
	c *fiber.Ctx,
	status int,
	title string,
	detail string,
	issue *radarrFieldError,
) error {
	c.Set(fiber.HeaderContentType, "application/problem+json")
	return c.Status(status).JSON(radarrProblemResponse{
		problemDetails: problemDetails{Type: "about:blank", Title: title, Status: status, Detail: detail},
		Issues:         []radarrIssueResponse{{Field: issue.Field, Message: issue.Message}},
	})
}

func parseRadarrInstanceRequest(c *fiber.Ctx) (radarrInstanceDraft, error) {
	var request radarrInstanceRequest
	if err := c.BodyParser(&request); err != nil {
		return radarrInstanceDraft{}, writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	return radarrInstanceDraft(request), nil
}

func parseRadarrPresetRequest(c *fiber.Ctx) (radarrPresetDraft, error) {
	var request radarrPresetRequest
	if err := c.BodyParser(&request); err != nil {
		return radarrPresetDraft{}, writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	return radarrPresetDraft(request), nil
}

func parseRadarrWebhookRequest(c *fiber.Ctx) (radarrWebhookDraft, error) {
	var request radarrWebhookRequest
	if err := c.BodyParser(&request); err != nil {
		return radarrWebhookDraft{}, writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}
	return radarrWebhookDraft{
		Name: request.Name, Kind: request.Format, URL: request.URL,
		Enabled: request.Enabled, Reasons: request.Reasons,
		RoleMention: request.RoleMention, Revision: request.Revision,
	}, nil
}

func radarrPathID(c *fiber.Ctx, name string) (int64, error) {
	raw := strings.TrimSpace(c.Params(name))
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", domain.ErrInvalidInput, name)
	}
	return id, nil
}

func toRadarrInstanceResponse(instance repository.RadarrInstance) radarrInstanceResponse {
	return radarrInstanceResponse{
		ID: instance.ID, Name: instance.Name, URL: instance.BaseURL,
		State: instance.State, Reason: instance.StateReason,
		APIKeyConfigured: len(instance.EncryptedAPIKey) > 0,
		LastTestedAt:     formatTimeValue(instance.LastCheckedAt),
		ArchivedAt:       formatTime(instance.ArchivedAt), Revision: instance.Revision,
		Used: instance.Used,
	}
}

func toRadarrPresetResponse(preset repository.RadarrPreset) radarrPresetResponse {
	tags := toRadarrTags(preset.Tags)
	tagIDs := make([]int, 0, len(preset.Tags))
	for _, tag := range preset.Tags {
		tagIDs = append(tagIDs, tag.ID)
	}
	return radarrPresetResponse{
		ID: preset.ID, Name: preset.Name, InstanceID: preset.InstanceID,
		InstanceName: preset.InstanceName, RootFolderPath: preset.RootFolderPath,
		QualityProfileID: preset.QualityProfileID, QualityProfileName: preset.QualityProfileName,
		TagIDs: tagIDs, Tags: tags, MinimumAvailability: preset.MinimumAvailability,
		Mode: preset.AcquisitionMode, Valid: preset.Valid,
		InvalidReason: preset.ValidationReason, ArchivedAt: formatTime(preset.ArchivedAt),
		Revision: preset.Revision, Used: preset.Used,
	}
}

func toRadarrWebhookResponse(destination repository.RadarrWebhookDestination) radarrWebhookResponse {
	health := "healthy"
	if destination.HealthWarningAt != nil {
		health = "warning"
	}
	return radarrWebhookResponse{
		ID: destination.ID, Name: destination.Name, Format: destination.Kind,
		Enabled: destination.Enabled, Verified: destination.VerifiedAt != nil,
		Reasons:     append(make([]string, 0, len(destination.ReasonFilters)), destination.ReasonFilters...),
		RoleMention: destination.DiscordRoleMention, Health: health,
		HealthReason: destination.HealthWarningReason,
		LastTestedAt: formatTime(destination.VerifiedAt), ArchivedAt: formatTime(destination.ArchivedAt),
		Revision: destination.Revision,
	}
}

func toRadarrAcquisitionResponse(acquisition repository.RadarrAcquisition) radarrAcquisitionResponse {
	response := radarrAcquisitionResponse{
		ID: acquisition.ID, MovieID: acquisition.MovieID, Title: acquisition.MovieTitle, Year: acquisition.MovieYear,
		Status: acquisition.Status, MutationState: acquisition.MutationState,
		ActionReason: acquisition.ActionReason,
		TargetLocked: acquisition.TargetLocked(), RadarrMovieID: valueOrZero(acquisition.RadarrMovieID),
		TargetPreviewedAt:     formatTime(acquisition.TargetPreviewedAt),
		TargetPreviewExisting: acquisition.TargetPreviewExisting,
		PreviewReady:          acquisition.TargetPreviewedAt != nil,
		ActiveQueue: acquisition.QueueStatus == "queued" || acquisition.QueueStatus == "downloading" ||
			acquisition.QueueStatus == "importing" || acquisition.QueueStatus == "failed",
		ManualAttemptCount: acquisition.ManualAttemptCount,
		LatestFailure:      acquisition.LatestFailureSummary,
		AbandonmentReason:  acquisition.AbandonmentReason,
		CreatedAt:          formatTimeValue(acquisition.CreatedAt), UpdatedAt: formatTimeValue(acquisition.UpdatedAt),
		Milestones: radarrAcquisitionMilestonesResponse{
			CreatedAt: formatTimeValue(acquisition.CreatedAt), RevealedAt: formatTime(acquisition.RevealedAt),
			TargetSelectedAt: formatTime(acquisition.TargetSelectedAt), AddedAt: formatTime(acquisition.TargetLockedAt),
			GrabbedAt: formatTime(acquisition.LatestReleaseSelectedAt), DownloadedAt: formatTime(acquisition.DownloadedAt),
			AbandonedAt: formatTime(acquisition.AbandonedAt), UpdatedAt: formatTimeValue(acquisition.UpdatedAt),
		},
	}
	if acquisition.Status == "action_needed" {
		response.ActionMessage = acquisition.LatestFailureSummary
	}
	if tmdbID, ok := acquisition.ResolvedTMDBID(); ok || acquisition.IMDbID != "" {
		response.Identity = &radarrIdentityResponse{
			TMDBID: tmdbID, IMDbID: acquisition.IMDbID,
			Title: acquisition.MovieTitle, Year: acquisition.MovieYear, Source: acquisition.IdentitySource,
		}
	}
	if acquisition.TargetInstanceID != nil {
		target := radarrTargetResponse{
			PresetID: valueOrZero(acquisition.PresetID), PresetName: acquisition.PresetName,
			InstanceID: *acquisition.TargetInstanceID, InstanceName: acquisition.TargetInstanceName,
			RootFolderPath:      acquisition.TargetRootFolderPath,
			QualityProfileID:    valueOrZero(acquisition.TargetQualityProfileID),
			QualityProfileName:  acquisition.TargetQualityProfileName,
			Tags:                toRadarrTags(acquisition.TargetTags),
			MinimumAvailability: acquisition.TargetMinimumAvailability,
			Mode:                acquisition.TargetAcquisitionMode,
		}
		response.Preset = &target
		response.Target = &target
	}
	response.Existing = acquisition.AdoptedExisting
	if !acquisition.TargetLocked() {
		response.Existing = acquisition.TargetPreviewExisting
	}
	response.ExistingMovie = response.Existing
	response.AdoptedExisting = acquisition.AdoptedExisting
	if acquisition.TargetLocked() || acquisition.TargetPreviewedAt != nil {
		response.EffectiveConfig = &radarrEffectiveConfigResponse{
			RootFolderPath:      acquisition.EffectiveConfiguration.RootFolderPath,
			QualityProfileID:    acquisition.EffectiveConfiguration.QualityProfileID,
			QualityProfileName:  acquisition.EffectiveConfiguration.QualityProfileName,
			Tags:                toRadarrTags(acquisition.EffectiveConfiguration.Tags),
			MinimumAvailability: acquisition.EffectiveConfiguration.MinimumAvailability,
			Monitored:           acquisition.EffectiveConfiguration.Monitored,
		}
	}
	if acquisition.LatestReleaseTitle != "" || acquisition.LatestReleaseSelectedAt != nil {
		response.LatestRelease = &radarrReleaseSummaryResponse{
			Title: acquisition.LatestReleaseTitle, Quality: acquisition.LatestReleaseQuality,
			SelectedAt: formatTime(acquisition.LatestReleaseSelectedAt),
		}
	}
	return response
}

func toRadarrReleaseResponse(release integrationradarr.Release, now time.Time) radarrReleaseResponse {
	ageHours := 0.0
	if !release.PublishedAt.IsZero() && release.PublishedAt.Before(now) {
		ageHours = math.Round(now.Sub(release.PublishedAt).Hours()*10) / 10
	}
	return radarrReleaseResponse{
		ID: release.ID, Title: release.Title, Quality: release.Quality.Name,
		Size: release.Size, AgeHours: ageHours, Peers: release.Seeders,
		Protocol: release.Protocol, Indexer: release.Indexer,
		CustomFormats:     append([]string(nil), release.CustomFormats...),
		CustomFormatScore: release.CustomFormatScore,
		Approved:          release.Approved, Rejected: release.Rejected,
		Rejections: append([]string(nil), release.RejectionReasons...),
		Mapped:     true, GrabAllowed: release.ID != "",
	}
}

func toRadarrTags(tags []repository.RadarrTagSnapshot) []radarrTagResponse {
	result := make([]radarrTagResponse, 0, len(tags))
	for _, tag := range tags {
		result = append(result, radarrTagResponse{ID: tag.ID, Label: tag.Label})
	}
	return result
}

func formatTimeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
