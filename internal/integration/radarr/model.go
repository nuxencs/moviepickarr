// Package radarr provides the typed seam between moviepickarr's Acquisition
// workflow and one Radarr instance.
package radarr

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var ErrInvalidInput = errors.New("invalid Radarr input")

var (
	ErrAuthentication   = errors.New("radarr authentication failed")
	ErrNotFound         = errors.New("radarr resource not found")
	ErrConflict         = errors.New("radarr request conflicted with remote state")
	ErrValidation       = errors.New("radarr rejected the request")
	ErrTransient        = errors.New("radarr is temporarily unavailable")
	ErrRemote           = errors.New("radarr request failed")
	ErrInvalidResponse  = errors.New("radarr returned an invalid response")
	ErrResponseTooLarge = errors.New("radarr response exceeded the configured limit")
	ErrReleaseExpired   = errors.New("interactive search result expired")
	ErrRejectedRelease  = errors.New("release requires explicit rejection override")
)

type RequestError struct {
	StatusCode int
	Kind       error
}

func (e *RequestError) Error() string {
	if e.StatusCode == 0 {
		return e.Kind.Error()
	}
	return fmt.Sprintf("%s (status %d)", e.Kind, e.StatusCode)
}

func (e *RequestError) Unwrap() error { return e.Kind }

type IdentityKind string

const (
	IdentityTMDB IdentityKind = "tmdb"
	IdentityIMDb IdentityKind = "imdb"
)

// ExactIdentity names exactly one external provider identity. Construct values
// with TMDBIdentity or IMDbIdentity so callers cannot accidentally fall back to
// title matching for an exact lookup.
type ExactIdentity struct {
	Kind   IdentityKind
	TMDBID int
	IMDbID string
}

type Catalog struct {
	Version         string
	InstanceName    string
	RootFolders     []RootFolder
	QualityProfiles []QualityProfile
	Tags            []Tag
}

type RootFolder struct {
	ID         int
	Path       string
	Accessible bool
	FreeSpace  int64
}

type QualityProfile struct {
	ID   int
	Name string
}

type Tag struct {
	ID    int
	Label string
}

// SecretCodec is satisfied by integration.SecretStore without coupling this
// typed module to its concrete implementation.
type SecretCodec interface {
	Encrypt(string) ([]byte, error)
	Decrypt([]byte) (string, error)
}

// Client is the complete Radarr seam used by Acquisition orchestration. The
// HTTP adapter keeps Radarr wire resources, credentials, and release keys
// behind this interface.
type Client interface {
	VerifyAndCatalog(context.Context) (Catalog, error)
	LookupMovie(context.Context, ExactIdentity) (MovieCandidate, error)
	SearchMovies(context.Context, TitleQuery) ([]MovieCandidate, error)
	FindMovieByTMDB(context.Context, int) (*Movie, error)
	AddMovie(context.Context, AddMovieRequest) (Movie, error)
	GetMovie(context.Context, int) (Movie, error)
	Queue(context.Context, int) ([]QueueItem, error)
	History(context.Context, int) ([]HistoryItem, error)
	SearchReleases(context.Context, int) ([]Release, error)
	GrabRelease(context.Context, GrabReleaseRequest) error
	SetMonitored(context.Context, int, bool) (Movie, error)
	StartMoviesSearch(context.Context, int) (Command, error)
	FindRecentMoviesSearchCommand(context.Context, int, time.Time) (*Command, error)
	GetCommand(context.Context, int) (Command, error)
}

var _ Client = (*HTTPClient)(nil)

type MovieCandidate struct {
	TMDBID int
	IMDbID string
	Title  string
	Year   int
}

type TitleQuery struct {
	Title string
	Year  int
}

func (q TitleQuery) validate() error {
	if strings.TrimSpace(q.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	if q.Year != 0 && (q.Year < 1870 || q.Year > 2100) {
		return fmt.Errorf("%w: year must be between 1870 and 2100", ErrInvalidInput)
	}
	return nil
}

type MinimumAvailability string

const (
	AvailabilityTBA       MinimumAvailability = "tba"
	AvailabilityAnnounced MinimumAvailability = "announced"
	AvailabilityInCinemas MinimumAvailability = "inCinemas"
	AvailabilityReleased  MinimumAvailability = "released"
)

type AcquisitionMode string

const (
	AcquisitionModeManual    AcquisitionMode = "manual"
	AcquisitionModeAutomatic AcquisitionMode = "automatic"
)

type AddMovieRequest struct {
	TMDBID              int
	Title               string
	RootFolderPath      string
	QualityProfileID    int
	TagIDs              []int
	MinimumAvailability MinimumAvailability
	Mode                AcquisitionMode
}

func (r AddMovieRequest) validate() error {
	if r.TMDBID <= 0 || strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.RootFolderPath) == "" || r.QualityProfileID <= 0 {
		return fmt.Errorf("%w: movie identity, title, root folder, and quality profile are required", ErrInvalidInput)
	}
	for _, id := range r.TagIDs {
		if id <= 0 {
			return fmt.Errorf("%w: tag IDs must be positive", ErrInvalidInput)
		}
	}
	switch r.MinimumAvailability {
	case AvailabilityTBA, AvailabilityAnnounced, AvailabilityInCinemas, AvailabilityReleased:
	default:
		return fmt.Errorf("%w: minimum availability is invalid", ErrInvalidInput)
	}
	switch r.Mode {
	case AcquisitionModeManual, AcquisitionModeAutomatic:
	default:
		return fmt.Errorf("%w: Acquisition mode is invalid", ErrInvalidInput)
	}
	return nil
}

type Movie struct {
	ID                  int
	TMDBID              int
	IMDbID              string
	Title               string
	Year                int
	Monitored           bool
	HasFile             bool
	RootFolderPath      string
	QualityProfileID    int
	TagIDs              []int
	MinimumAvailability MinimumAvailability
}

type Quality struct {
	Name     string
	Revision int
	Repack   bool
}

type QueueItem struct {
	ID                      int
	MovieID                 int
	Title                   string
	Status                  string
	TrackedDownloadStatus   string
	TrackedDownloadState    string
	Protocol                string
	Indexer                 string
	Size                    float64
	SizeRemaining           float64
	EstimatedCompletionTime *time.Time
	Quality                 Quality
}

type HistoryItem struct {
	ID                int
	MovieID           int
	EventType         string
	SourceTitle       string
	Date              time.Time
	Quality           Quality
	CustomFormatScore int
}

type Release struct {
	// ID is a short-lived opaque handle. It contains no Radarr GUID, URL,
	// magnet, hash, or indexer credential.
	ID                string
	Title             string
	Indexer           string
	Size              int64
	PublishedAt       time.Time
	Protocol          string
	Seeders           *int
	Leechers          *int
	Quality           Quality
	Languages         []string
	CustomFormats     []string
	CustomFormatScore int
	ReleaseGroup      string
	Edition           string
	Approved          bool
	Rejected          bool
	RejectionReasons  []string
}

type GrabReleaseRequest struct {
	ResultID      string
	AllowRejected bool
}

type Command struct {
	ID      int
	Name    string
	Status  string
	Message string
	Queued  time.Time
	Started *time.Time
	Ended   *time.Time
}

func TMDBIdentity(id int) (ExactIdentity, error) {
	if id <= 0 {
		return ExactIdentity{}, fmt.Errorf("%w: TMDB ID must be positive", ErrInvalidInput)
	}
	return ExactIdentity{Kind: IdentityTMDB, TMDBID: id}, nil
}

var imdbIDPattern = regexp.MustCompile(`^tt\d{7,8}$`)

func IMDbIdentity(id string) (ExactIdentity, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !imdbIDPattern.MatchString(id) {
		return ExactIdentity{}, fmt.Errorf("%w: IMDb ID must use the tt1234567 form", ErrInvalidInput)
	}
	return ExactIdentity{Kind: IdentityIMDb, IMDbID: id}, nil
}

func (i ExactIdentity) validate() error {
	switch i.Kind {
	case IdentityTMDB:
		if i.TMDBID <= 0 || i.IMDbID != "" {
			return fmt.Errorf("%w: invalid TMDB identity", ErrInvalidInput)
		}
	case IdentityIMDb:
		if i.TMDBID != 0 || !imdbIDPattern.MatchString(i.IMDbID) {
			return fmt.Errorf("%w: invalid IMDb identity", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: unknown identity kind", ErrInvalidInput)
	}
	return nil
}
