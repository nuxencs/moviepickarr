package radarr

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type moviePayload struct {
	ID                  int                `json:"id"`
	TMDBID              int                `json:"tmdbId"`
	IMDbID              string             `json:"imdbId"`
	Title               string             `json:"title"`
	Year                int                `json:"year"`
	Monitored           bool               `json:"monitored"`
	HasFile             bool               `json:"hasFile"`
	RootFolderPath      string             `json:"rootFolderPath"`
	QualityProfileID    int                `json:"qualityProfileId"`
	Tags                []int              `json:"tags"`
	MinimumAvailability string             `json:"minimumAvailability"`
	AddOptions          *addOptionsPayload `json:"addOptions,omitzero"`
}

func (c *HTTPClient) FindMovieByTMDB(ctx context.Context, tmdbID int) (*Movie, error) {
	if tmdbID <= 0 {
		return nil, fmt.Errorf("%w: TMDB ID must be positive", ErrInvalidInput)
	}
	values := url.Values{"tmdbId": []string{strconv.Itoa(tmdbID)}}
	var movies []moviePayload
	if err := c.get(ctx, "movie", values, &movies); err != nil {
		return nil, err
	}
	if len(movies) == 0 {
		return nil, nil
	}
	if len(movies) != 1 || movies[0].TMDBID != tmdbID {
		return nil, fmt.Errorf("%w: exact TMDB lookup returned an unexpected movie set", ErrInvalidResponse)
	}
	movie := movieFromPayload(movies[0])
	return &movie, nil
}

func (c *HTTPClient) GetMovie(ctx context.Context, movieID int) (Movie, error) {
	if movieID <= 0 {
		return Movie{}, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	var movie moviePayload
	if err := c.get(ctx, "movie/"+strconv.Itoa(movieID), nil, &movie); err != nil {
		return Movie{}, err
	}
	if movie.ID != movieID {
		return Movie{}, fmt.Errorf("%w: movie response ID does not match the request", ErrInvalidResponse)
	}
	return movieFromPayload(movie), nil
}

func (c *HTTPClient) AddMovie(ctx context.Context, request AddMovieRequest) (Movie, error) {
	if err := request.validate(); err != nil {
		return Movie{}, err
	}
	availability, err := wireAvailability(request.MinimumAvailability)
	if err != nil {
		return Movie{}, err
	}
	monitored := request.Mode == AcquisitionModeAutomatic
	monitor := "none"
	if monitored {
		monitor = "movieOnly"
	}
	payload := moviePayload{
		TMDBID:              request.TMDBID,
		Title:               strings.TrimSpace(request.Title),
		Monitored:           monitored,
		RootFolderPath:      strings.TrimSpace(request.RootFolderPath),
		QualityProfileID:    request.QualityProfileID,
		Tags:                append([]int(nil), request.TagIDs...),
		MinimumAvailability: availability,
		AddOptions:          &addOptionsPayload{Monitor: monitor, SearchForMovie: false},
	}
	var added moviePayload
	if err := c.post(ctx, "movie", payload, &added); err != nil {
		return Movie{}, err
	}
	if added.ID <= 0 || added.TMDBID != request.TMDBID {
		return Movie{}, fmt.Errorf("%w: added movie response is missing its identity", ErrInvalidResponse)
	}
	return movieFromPayload(added), nil
}

func (c *HTTPClient) SetMonitored(ctx context.Context, movieID int, monitored bool) (Movie, error) {
	if movieID <= 0 {
		return Movie{}, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	endpoint := "movie/" + strconv.Itoa(movieID)
	var remote map[string]jsontext.Value
	if err := c.get(ctx, endpoint, nil, &remote); err != nil {
		return Movie{}, err
	}
	var remoteID int
	if err := json.Unmarshal(remote["id"], &remoteID); err != nil || remoteID != movieID {
		return Movie{}, fmt.Errorf("%w: movie response ID does not match the request", ErrInvalidResponse)
	}
	remote["monitored"] = jsontext.Value(strconv.FormatBool(monitored))
	var updated moviePayload
	if err := c.put(ctx, endpoint, remote, &updated); err != nil {
		return Movie{}, err
	}
	if updated.ID != movieID {
		return Movie{}, fmt.Errorf("%w: updated movie response ID does not match the request", ErrInvalidResponse)
	}
	return movieFromPayload(updated), nil
}

type addOptionsPayload struct {
	Monitor        string `json:"monitor"`
	SearchForMovie bool   `json:"searchForMovie"`
}

func (c *HTTPClient) LookupMovie(ctx context.Context, identity ExactIdentity) (MovieCandidate, error) {
	if err := identity.validate(); err != nil {
		return MovieCandidate{}, err
	}
	query := url.Values{}
	endpoint := "movie/lookup/tmdb"
	switch identity.Kind {
	case IdentityTMDB:
		query.Set("tmdbId", strconv.Itoa(identity.TMDBID))
	case IdentityIMDb:
		endpoint = "movie/lookup/imdb"
		query.Set("imdbId", identity.IMDbID)
	}
	var movie moviePayload
	if err := c.get(ctx, endpoint, query, &movie); err != nil {
		return MovieCandidate{}, err
	}
	switch identity.Kind {
	case IdentityTMDB:
		if movie.TMDBID != identity.TMDBID {
			return MovieCandidate{}, fmt.Errorf("%w: TMDB lookup returned a different movie", ErrInvalidResponse)
		}
	case IdentityIMDb:
		if !strings.EqualFold(strings.TrimSpace(movie.IMDbID), identity.IMDbID) {
			return MovieCandidate{}, fmt.Errorf("%w: IMDb lookup returned a different movie", ErrInvalidResponse)
		}
	}
	return candidateFromPayload(movie), nil
}

func (c *HTTPClient) SearchMovies(ctx context.Context, query TitleQuery) ([]MovieCandidate, error) {
	if err := query.validate(); err != nil {
		return nil, err
	}
	term := strings.TrimSpace(query.Title)
	if query.Year != 0 {
		term += " " + strconv.Itoa(query.Year)
	}
	values := url.Values{"term": []string{term}}
	var movies []moviePayload
	if err := c.get(ctx, "movie/lookup", values, &movies); err != nil {
		return nil, err
	}
	result := make([]MovieCandidate, 0, len(movies))
	for _, movie := range movies {
		if movie.TMDBID <= 0 || strings.TrimSpace(movie.Title) == "" {
			continue
		}
		result = append(result, candidateFromPayload(movie))
	}
	return result, nil
}

func candidateFromPayload(movie moviePayload) MovieCandidate {
	return MovieCandidate{TMDBID: movie.TMDBID, IMDbID: movie.IMDbID, Title: movie.Title, Year: movie.Year}
}

func movieFromPayload(movie moviePayload) Movie {
	return Movie{
		ID: movie.ID, TMDBID: movie.TMDBID, IMDbID: movie.IMDbID, Title: movie.Title, Year: movie.Year,
		Monitored: movie.Monitored, HasFile: movie.HasFile, RootFolderPath: movie.RootFolderPath,
		QualityProfileID: movie.QualityProfileID, TagIDs: append([]int(nil), movie.Tags...),
		MinimumAvailability: normalizeAvailability(movie.MinimumAvailability),
	}
}

func normalizeAvailability(value string) MinimumAvailability {
	switch strings.ToLower(strings.ReplaceAll(value, "_", "")) {
	case "tba":
		return AvailabilityTBA
	case "announced":
		return AvailabilityAnnounced
	case "incinemas":
		return AvailabilityInCinemas
	case "released":
		return AvailabilityReleased
	default:
		return MinimumAvailability(value)
	}
}

func wireAvailability(value MinimumAvailability) (string, error) {
	switch value {
	case AvailabilityTBA:
		return "tba", nil
	case AvailabilityAnnounced:
		return "announced", nil
	case AvailabilityInCinemas:
		return "inCinemas", nil
	case AvailabilityReleased:
		return "released", nil
	default:
		return "", fmt.Errorf("%w: minimum availability is invalid", ErrInvalidInput)
	}
}
