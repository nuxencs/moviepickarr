package radarr_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/integration/radarr"
)

func TestHTTPClientVerifyAndCatalogUsesV3APIKeyAndReturnsTypedSetup(t *testing.T) {
	t.Parallel()

	wantedPaths := map[string]any{
		"/radarr/api/v3/system/status":  map[string]any{"version": "6.4.1", "instanceName": "Movies"},
		"/radarr/api/v3/rootfolder":     []map[string]any{{"id": 2, "path": "/media/movies", "accessible": true, "freeSpace": 1234}},
		"/radarr/api/v3/qualityprofile": []map[string]any{{"id": 3, "name": "HD-1080p"}},
		"/radarr/api/v3/tag":            []map[string]any{{"id": 4, "label": "moviepickarr"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Api-Key"); got != "secret" {
			t.Errorf("X-Api-Key = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		payload, ok := wantedPaths[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		delete(wantedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, payload)
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{
		BaseURL: server.URL + "/radarr/",
		APIKey:  "secret",
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	catalog, err := client.VerifyAndCatalog(context.Background())
	if err != nil {
		t.Fatalf("VerifyAndCatalog: %v", err)
	}
	if catalog.Version != "6.4.1" || catalog.InstanceName != "Movies" {
		t.Fatalf("status = %+v", catalog)
	}
	if len(catalog.RootFolders) != 1 || catalog.RootFolders[0].ID != 2 || catalog.RootFolders[0].Path != "/media/movies" || !catalog.RootFolders[0].Accessible {
		t.Fatalf("root folders = %+v", catalog.RootFolders)
	}
	if len(catalog.QualityProfiles) != 1 || catalog.QualityProfiles[0].Name != "HD-1080p" {
		t.Fatalf("quality profiles = %+v", catalog.QualityProfiles)
	}
	if len(catalog.Tags) != 1 || catalog.Tags[0].Label != "moviepickarr" {
		t.Fatalf("tags = %+v", catalog.Tags)
	}
	if len(wantedPaths) != 0 {
		t.Fatalf("unrequested paths = %v", wantedPaths)
	}
}

func TestHTTPClientSupportsExactIdentityAndExplicitTitleLookups(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/movie/lookup/tmdb":
			if got := r.URL.Query().Get("tmdbId"); got != "27205" {
				t.Errorf("tmdbId = %q", got)
			}
			_, _ = w.Write([]byte(`{"tmdbId":27205,"imdbId":"tt1375666","title":"Inception","year":2010}`))
		case "/api/v3/movie/lookup/imdb":
			if got := r.URL.Query().Get("imdbId"); got != "tt1375666" {
				t.Errorf("imdbId = %q", got)
			}
			_, _ = w.Write([]byte(`{"tmdbId":27205,"imdbId":"tt1375666","title":"Inception","year":2010}`))
		case "/api/v3/movie/lookup":
			if got := r.URL.Query().Get("term"); got != "Inception 2010" {
				t.Errorf("term = %q", got)
			}
			_, _ = w.Write([]byte(`[{"tmdbId":27205,"imdbId":"tt1375666","title":"Inception","year":2010}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	tmdbIdentity, _ := radarr.TMDBIdentity(27205)
	byTMDB, err := client.LookupMovie(context.Background(), tmdbIdentity)
	if err != nil || byTMDB.TMDBID != 27205 {
		t.Fatalf("TMDB lookup = %+v, %v", byTMDB, err)
	}
	imdbIdentity, _ := radarr.IMDbIdentity("tt1375666")
	byIMDb, err := client.LookupMovie(context.Background(), imdbIdentity)
	if err != nil || byIMDb.IMDbID != "tt1375666" {
		t.Fatalf("IMDb lookup = %+v, %v", byIMDb, err)
	}
	byTitle, err := client.SearchMovies(context.Background(), radarr.TitleQuery{Title: "Inception", Year: 2010})
	if err != nil || len(byTitle) != 1 || byTitle[0].Title != "Inception" {
		t.Fatalf("title lookup = %+v, %v", byTitle, err)
	}
}

func TestHTTPClientRejectsMismatchedExactLookupResponses(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/movie/lookup/tmdb":
			_, _ = w.Write([]byte(`{"tmdbId":999,"imdbId":"tt9999999","title":"Wrong"}`))
		case "/api/v3/movie/lookup/imdb":
			_, _ = w.Write([]byte(`{"tmdbId":999,"imdbId":"tt9999999","title":"Wrong"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	tmdbIdentity, _ := radarr.TMDBIdentity(27205)
	if _, err := client.LookupMovie(context.Background(), tmdbIdentity); !errors.Is(err, radarr.ErrInvalidResponse) {
		t.Fatalf("mismatched TMDB lookup error = %v", err)
	}
	imdbIdentity, _ := radarr.IMDbIdentity("tt1375666")
	if _, err := client.LookupMovie(context.Background(), imdbIdentity); !errors.Is(err, radarr.ErrInvalidResponse) {
		t.Fatalf("mismatched IMDb lookup error = %v", err)
	}
}

func TestHTTPClientFindsAddsAndGetsMoviesWithTypedAcquisitionModes(t *testing.T) {
	t.Parallel()

	var addBodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie" && r.URL.Query().Get("tmdbId") == "27205":
			_, _ = w.Write([]byte(`[{"id":8,"tmdbId":27205,"imdbId":"tt1375666","title":"Inception","year":2010,"monitored":false,"hasFile":false,"rootFolderPath":"/media/movies","qualityProfileId":3,"tags":[4],"minimumAvailability":"inCinemas"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie" && r.URL.Query().Get("tmdbId") == "999":
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/movie/8":
			_, _ = w.Write([]byte(`{"id":8,"tmdbId":27205,"title":"Inception","hasFile":true,"minimumAvailability":"released"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/movie":
			var body map[string]any
			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				t.Errorf("decode add body: %v", err)
			}
			addBodies = append(addBodies, body)
			body["id"] = len(addBodies) + 20
			_ = json.MarshalWrite(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	existing, err := client.FindMovieByTMDB(context.Background(), 27205)
	if err != nil || existing == nil || existing.ID != 8 || existing.MinimumAvailability != radarr.AvailabilityInCinemas {
		t.Fatalf("existing movie = %+v, %v", existing, err)
	}
	missing, err := client.FindMovieByTMDB(context.Background(), 999)
	if err != nil || missing != nil {
		t.Fatalf("missing movie = %+v, %v", missing, err)
	}
	got, err := client.GetMovie(context.Background(), 8)
	if err != nil || !got.HasFile {
		t.Fatalf("GetMovie = %+v, %v", got, err)
	}

	base := radarr.AddMovieRequest{
		TMDBID: 27205, Title: "Inception", RootFolderPath: "/media/movies",
		QualityProfileID: 3, TagIDs: []int{4}, MinimumAvailability: radarr.AvailabilityInCinemas,
	}
	manual := base
	manual.Mode = radarr.AcquisitionModeManual
	if _, err := client.AddMovie(context.Background(), manual); err != nil {
		t.Fatalf("manual AddMovie: %v", err)
	}
	automatic := base
	automatic.Mode = radarr.AcquisitionModeAutomatic
	if _, err := client.AddMovie(context.Background(), automatic); err != nil {
		t.Fatalf("automatic AddMovie: %v", err)
	}
	if len(addBodies) != 2 {
		t.Fatalf("add bodies = %d", len(addBodies))
	}
	if addBodies[0]["monitored"] != false || addBodies[1]["monitored"] != true {
		t.Fatalf("monitored flags = manual %v, automatic %v", addBodies[0]["monitored"], addBodies[1]["monitored"])
	}
	for i, body := range addBodies {
		if body["minimumAvailability"] != "inCinemas" {
			t.Fatalf("add body %d availability = %#v", i, body["minimumAvailability"])
		}
		options, ok := body["addOptions"].(map[string]any)
		if !ok || options["searchForMovie"] != false {
			t.Fatalf("add body %d options = %#v", i, body["addOptions"])
		}
	}
}

func TestHTTPClientSetMonitoredPreservesTheRemoteMovieConfiguration(t *testing.T) {
	t.Parallel()

	var updated map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":8,"tmdbId":27205,"title":"Inception","monitored":false,"path":"/media/movies/Inception (2010)","rootFolderPath":"/media/movies","qualityProfileId":3,"tags":[4,5],"minimumAvailability":"released","customField":"preserve-me"}`))
		case http.MethodPut:
			if err := json.UnmarshalRead(r.Body, &updated); err != nil {
				t.Errorf("decode update: %v", err)
			}
			_ = json.MarshalWrite(w, updated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	movie, err := client.SetMonitored(context.Background(), 8, true)
	if err != nil {
		t.Fatalf("SetMonitored: %v", err)
	}
	if !movie.Monitored || updated["monitored"] != true || updated["customField"] != "preserve-me" || updated["qualityProfileId"] != float64(3) {
		t.Fatalf("updated movie = %+v, wire = %#v", movie, updated)
	}
}

func TestHTTPClientReturnsSanitizedQueueAndHistoryViews(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if got := r.URL.Query().Get("movieId"); got != "8" {
			t.Errorf("movieId = %q", got)
		}
		switch r.URL.Path {
		case "/api/v3/queue/details":
			_, _ = w.Write([]byte(`[{"id":12,"movieId":8,"title":"Inception.2010","status":"downloading","trackedDownloadStatus":"ok","trackedDownloadState":"downloading","protocol":"torrent","indexer":"Example","size":1000.5,"sizeleft":250.25,"estimatedCompletionTime":"2026-08-07T20:00:00Z","quality":{"quality":{"name":"Bluray-1080p"},"revision":{"version":2,"isRepack":true}},"downloadId":"secret-download-id","outputPath":"/private/path"}]`))
		case "/api/v3/history/movie":
			_, _ = w.Write([]byte(`[{"id":13,"movieId":8,"eventType":"grabbed","sourceTitle":"Inception.2010","date":"2026-08-07T19:00:00Z","quality":{"quality":{"name":"Bluray-1080p"}},"customFormatScore":1200,"downloadId":"secret-download-id","data":{"downloadUrl":"secret"}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	queue, err := client.Queue(context.Background(), 8)
	if err != nil || len(queue) != 1 || queue[0].Size != 1000.5 || queue[0].SizeRemaining != 250.25 || queue[0].Quality.Name != "Bluray-1080p" || !queue[0].Quality.Repack {
		t.Fatalf("Queue = %+v, %v", queue, err)
	}
	history, err := client.History(context.Background(), 8)
	if err != nil || len(history) != 1 || history[0].EventType != "grabbed" || history[0].CustomFormatScore != 1200 {
		t.Fatalf("History = %+v, %v", history, err)
	}

	encodedQueue, _ := json.Marshal(queue)
	encodedHistory, _ := json.Marshal(history)
	for _, forbidden := range []string{"secret-download-id", "/private/path", "downloadUrl"} {
		if strings.Contains(string(encodedQueue), forbidden) || strings.Contains(string(encodedHistory), forbidden) {
			t.Fatalf("sanitized views contain %q: %s %s", forbidden, encodedQueue, encodedHistory)
		}
	}
}

func TestHTTPClientStartsAndReadsTrackableMoviesSearchCommand(t *testing.T) {
	t.Parallel()

	var commandBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			_ = json.UnmarshalRead(r.Body, &commandBody)
			_, _ = w.Write([]byte(`{"id":31,"name":"MoviesSearch","status":"queued","queued":"2026-08-07T19:00:00Z"}`))
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":31,"name":"MoviesSearch","status":"completed","message":"Completed","queued":"2026-08-07T19:00:00Z","started":"2026-08-07T19:00:01Z","ended":"2026-08-07T19:00:02Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	started, err := client.StartMoviesSearch(context.Background(), 8)
	if err != nil || started.ID != 31 || started.Status != "queued" {
		t.Fatalf("StartMoviesSearch = %+v, %v", started, err)
	}
	if commandBody["name"] != "MoviesSearch" {
		t.Fatalf("command body = %#v", commandBody)
	}
	ids, ok := commandBody["movieIds"].([]any)
	if !ok || len(ids) != 1 || ids[0] != float64(8) {
		t.Fatalf("movieIds = %#v", commandBody["movieIds"])
	}
	completed, err := client.GetCommand(context.Background(), 31)
	if err != nil || completed.Status != "completed" || completed.Started == nil || completed.Ended == nil {
		t.Fatalf("GetCommand = %+v, %v", completed, err)
	}
}

func TestHTTPClientFindsNewestExactRecentMoviesSearchCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/command" {
			http.NotFound(w, r)
			return
		}
		if r.URL.RawQuery != "" {
			t.Errorf("command query = %q, want empty", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":40,"name":"RefreshMovie","status":"completed","queued":"2026-08-07T19:02:00Z","ended":"2026-08-07T19:03:00Z","body":{"movieIds":[8]}},
			{"id":41,"name":"MoviesSearch","status":"completed","queued":"2026-08-07T19:02:00Z","ended":"2026-08-07T19:03:00Z","body":{"movieIds":[9]}},
			{"id":42,"name":"MoviesSearch","status":"completed","queued":"2026-08-07T19:02:00Z","ended":"2026-08-07T19:03:00Z","body":{"movieIds":[8,9]}},
			{"id":43,"name":"MoviesSearch","status":"completed","queued":"2026-08-07T18:00:00Z","ended":"2026-08-07T18:01:00Z","body":{"movieIds":[8]}},
			{"id":44,"name":"MoviesSearch","status":"completed","queued":"2026-08-07T19:01:00Z","ended":"2026-08-07T19:02:00Z","body":{"movieIds":[8]}},
			{"id":45,"name":"MoviesSearch","status":"completed","queued":"2026-08-07T19:02:00Z","ended":"2026-08-07T19:03:00Z","body":{"movieIds":[8]}}
		]`))
	}))
	defer server.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	boundary := time.Date(2026, time.August, 7, 19, 0, 0, 0, time.UTC)
	command, err := client.FindRecentMoviesSearchCommand(context.Background(), 8, boundary)
	if err != nil {
		t.Fatalf("FindRecentMoviesSearchCommand: %v", err)
	}
	if command == nil || command.ID != 45 || command.Name != "MoviesSearch" {
		t.Fatalf("FindRecentMoviesSearchCommand = %+v", command)
	}
}

func TestNewHTTPClientRejectsUnsafeBaseURLs(t *testing.T) {
	t.Parallel()

	for _, rawURL := range []string{
		"",
		"radarr.local",
		"ftp://radarr.local",
		"https://user:pass@radarr.local",
		"https://radarr.local?apiKey=leak",
		"https://radarr.local/#fragment",
	} {
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			if _, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: rawURL, APIKey: "secret"}); err == nil {
				t.Fatalf("NewHTTPClient(%q) unexpectedly succeeded", rawURL)
			}
		})
	}
}

func TestHTTPClientClassifiesRemoteStatuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status int
		want   error
	}{
		{status: http.StatusUnauthorized, want: radarr.ErrAuthentication},
		{status: http.StatusForbidden, want: radarr.ErrAuthentication},
		{status: http.StatusNotFound, want: radarr.ErrNotFound},
		{status: http.StatusConflict, want: radarr.ErrConflict},
		{status: http.StatusUnprocessableEntity, want: radarr.ErrValidation},
		{status: http.StatusTooManyRequests, want: radarr.ErrTransient},
		{status: http.StatusBadGateway, want: radarr.ErrTransient},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"message":"details must not affect classification"}`))
			}))
			defer server.Close()
			client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: server.URL, APIKey: "secret"})
			if err != nil {
				t.Fatalf("NewHTTPClient: %v", err)
			}
			_, err = client.VerifyAndCatalog(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHTTPClientRefusesCrossOriginRedirectWithoutLeakingAPIKey(t *testing.T) {
	t.Parallel()

	var leakedKey string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leakedKey = r.Header.Get("X-Api-Key")
		_, _ = w.Write([]byte(`{"version":"wrong server"}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusFound)
	}))
	defer source.Close()

	client, err := radarr.NewHTTPClient(radarr.ClientConfig{BaseURL: source.URL, APIKey: "secret"})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	_, err = client.VerifyAndCatalog(context.Background())
	if !errors.Is(err, radarr.ErrRemote) {
		t.Fatalf("redirect error = %v", err)
	}
	if leakedKey != "" {
		t.Fatalf("API key leaked to redirect target: %q", leakedKey)
	}
}

func TestHTTPClientBoundsResponseBodies(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"this response is deliberately too large"}`))
	}))
	defer server.Close()
	client, err := radarr.NewHTTPClient(radarr.ClientConfig{
		BaseURL: server.URL, APIKey: "secret", MaxResponseBytes: 16,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	_, err = client.VerifyAndCatalog(context.Background())
	if !errors.Is(err, radarr.ErrResponseTooLarge) {
		t.Fatalf("large response error = %v", err)
	}
}

func TestHTTPClientAppliesConfiguredTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"version":"late"}`))
	}))
	defer server.Close()
	client, err := radarr.NewHTTPClient(radarr.ClientConfig{
		BaseURL: server.URL, APIKey: "secret", Timeout: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	_, err = client.VerifyAndCatalog(context.Background())
	if !errors.Is(err, radarr.ErrTransient) {
		t.Fatalf("timeout error = %v", err)
	}
}
