package server

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"
	integrationradarr "moviepickarr/internal/integration/radarr"
	"moviepickarr/internal/repository"
)

type passthroughRadarrSecrets struct{}

func (passthroughRadarrSecrets) Encrypt(value string) ([]byte, error) {
	return []byte(value), nil
}

func (passthroughRadarrSecrets) Decrypt(value []byte) (string, error) {
	return string(value), nil
}

type monitoredRequest struct {
	movieID   int
	monitored bool
}

type fakeCallBarrier struct {
	arrived chan struct{}
	release chan struct{}
}

func newFakeCallBarrier() *fakeCallBarrier {
	return &fakeCallBarrier{
		arrived: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
}

type fakeRadarrClient struct {
	concurrentMu sync.Mutex
	catalog      integrationradarr.Catalog

	findMovie            *integrationradarr.Movie
	findErr              error
	lookupMovie          integrationradarr.MovieCandidate
	lookupErr            error
	searchMovies         []integrationradarr.MovieCandidate
	searchMoviesErr      error
	movies               map[int]integrationradarr.Movie
	getErrors            map[int]error
	queue                map[int][]integrationradarr.QueueItem
	queueErrors          map[int]error
	history              map[int][]integrationradarr.HistoryItem
	historyErrors        map[int]error
	releases             map[int][]integrationradarr.Release
	releaseErrors        map[int]error
	addResults           []integrationradarr.Movie
	addErr               error
	addLandsOnError      bool
	grabErr              error
	monitorErr           error
	searchCommand        integrationradarr.Command
	searchCommandErr     error
	searchLandsOnError   bool
	movieSearchCommands  map[int][]integrationradarr.Command
	movieSearchLookupErr error
	commands             map[int]integrationradarr.Command
	commandErrors        map[int]error
	findBarrier          *fakeCallBarrier
	addBarrier           *fakeCallBarrier
	grabBarrier          *fakeCallBarrier
	getMovieBarrier      *fakeCallBarrier
	queueBarrier         *fakeCallBarrier
	releaseBarrier       *fakeCallBarrier
	searchBarrier        *fakeCallBarrier
	onSetMonitored       func(int, bool)

	verifyCalls        int
	lookupCalls        int
	searchMovieCalls   int
	findCalls          int
	getCalls           []int
	queueCalls         []int
	historyCalls       []int
	releaseSearches    []int
	addRequests        []integrationradarr.AddMovieRequest
	grabRequests       []integrationradarr.GrabReleaseRequest
	monitorRequests    []monitoredRequest
	automaticSearches  []int
	movieSearchLookups []int
}

func newFakeRadarrClient() *fakeRadarrClient {
	return &fakeRadarrClient{
		catalog: integrationradarr.Catalog{
			Version: "5.7.0", InstanceName: "Radarr test",
			RootFolders: []integrationradarr.RootFolder{
				{ID: 10, Path: "/media/movies", Accessible: true},
				{ID: 90, Path: "/existing/movies", Accessible: true},
			},
			QualityProfiles: []integrationradarr.QualityProfile{
				{ID: 20, Name: "HD-1080p"},
				{ID: 91, Name: "Legacy profile"},
			},
			Tags: []integrationradarr.Tag{
				{ID: 30, Label: "movies"},
				{ID: 92, Label: "legacy"},
			},
		},
		movies:              make(map[int]integrationradarr.Movie),
		getErrors:           make(map[int]error),
		queue:               make(map[int][]integrationradarr.QueueItem),
		queueErrors:         make(map[int]error),
		history:             make(map[int][]integrationradarr.HistoryItem),
		historyErrors:       make(map[int]error),
		releases:            make(map[int][]integrationradarr.Release),
		releaseErrors:       make(map[int]error),
		commands:            make(map[int]integrationradarr.Command),
		commandErrors:       make(map[int]error),
		movieSearchCommands: make(map[int][]integrationradarr.Command),
		searchCommand:       integrationradarr.Command{ID: 700, Name: "MoviesSearch", Status: "queued"},
	}
}

func (f *fakeRadarrClient) VerifyAndCatalog(context.Context) (integrationradarr.Catalog, error) {
	f.verifyCalls++
	return f.catalog, nil
}

func (f *fakeRadarrClient) LookupMovie(context.Context, integrationradarr.ExactIdentity) (integrationradarr.MovieCandidate, error) {
	f.lookupCalls++
	return f.lookupMovie, f.lookupErr
}

func (f *fakeRadarrClient) SearchMovies(context.Context, integrationradarr.TitleQuery) ([]integrationradarr.MovieCandidate, error) {
	f.searchMovieCalls++
	return f.searchMovies, f.searchMoviesErr
}

func (f *fakeRadarrClient) FindMovieByTMDB(context.Context, int) (*integrationradarr.Movie, error) {
	f.concurrentMu.Lock()
	f.findCalls++
	barrier := f.findBarrier
	if f.findMovie == nil {
		err := f.findErr
		f.concurrentMu.Unlock()
		if barrier != nil {
			barrier.arrived <- struct{}{}
			<-barrier.release
		}
		return nil, err
	}
	movie := *f.findMovie
	movie.TagIDs = append([]int(nil), f.findMovie.TagIDs...)
	err := f.findErr
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	return &movie, err
}

func (f *fakeRadarrClient) AddMovie(_ context.Context, request integrationradarr.AddMovieRequest) (integrationradarr.Movie, error) {
	f.concurrentMu.Lock()
	f.addRequests = append(f.addRequests, request)
	barrier := f.addBarrier
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	f.concurrentMu.Lock()
	defer f.concurrentMu.Unlock()
	if f.addErr != nil && !f.addLandsOnError {
		return integrationradarr.Movie{}, f.addErr
	}
	var movie integrationradarr.Movie
	if len(f.addResults) > 0 {
		movie = f.addResults[0]
		f.addResults = f.addResults[1:]
	} else {
		movie = integrationradarr.Movie{ID: 100 + len(f.addRequests), TMDBID: request.TMDBID, Title: request.Title}
	}
	if movie.TMDBID == 0 {
		movie.TMDBID = request.TMDBID
	}
	if movie.Title == "" {
		movie.Title = request.Title
	}
	if movie.RootFolderPath == "" {
		movie.RootFolderPath = request.RootFolderPath
	}
	if movie.QualityProfileID == 0 {
		movie.QualityProfileID = request.QualityProfileID
	}
	if movie.TagIDs == nil {
		movie.TagIDs = append([]int(nil), request.TagIDs...)
	}
	if movie.MinimumAvailability == "" {
		movie.MinimumAvailability = request.MinimumAvailability
	}
	movie.Monitored = request.Mode == integrationradarr.AcquisitionModeAutomatic
	f.movies[movie.ID] = movie
	if f.addErr != nil {
		copy := movie
		f.findMovie = &copy
		return integrationradarr.Movie{}, f.addErr
	}
	return movie, nil
}

func (f *fakeRadarrClient) GetMovie(_ context.Context, id int) (integrationradarr.Movie, error) {
	f.concurrentMu.Lock()
	f.getCalls = append(f.getCalls, id)
	err := f.getErrors[id]
	movie, ok := f.movies[id]
	barrier := f.getMovieBarrier
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	if err != nil {
		return integrationradarr.Movie{}, err
	}
	if !ok {
		return integrationradarr.Movie{}, integrationradarr.ErrNotFound
	}
	return movie, nil
}

func (f *fakeRadarrClient) Queue(_ context.Context, movieID int) ([]integrationradarr.QueueItem, error) {
	f.concurrentMu.Lock()
	f.queueCalls = append(f.queueCalls, movieID)
	queue := append([]integrationradarr.QueueItem(nil), f.queue[movieID]...)
	err := f.queueErrors[movieID]
	barrier := f.queueBarrier
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	return queue, err
}

func (f *fakeRadarrClient) History(_ context.Context, movieID int) ([]integrationradarr.HistoryItem, error) {
	f.historyCalls = append(f.historyCalls, movieID)
	return append([]integrationradarr.HistoryItem(nil), f.history[movieID]...), f.historyErrors[movieID]
}

func (f *fakeRadarrClient) SearchReleases(_ context.Context, movieID int) ([]integrationradarr.Release, error) {
	f.concurrentMu.Lock()
	f.releaseSearches = append(f.releaseSearches, movieID)
	releases := append([]integrationradarr.Release(nil), f.releases[movieID]...)
	err := f.releaseErrors[movieID]
	barrier := f.releaseBarrier
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	return releases, err
}

func (f *fakeRadarrClient) GrabRelease(_ context.Context, request integrationradarr.GrabReleaseRequest) error {
	f.concurrentMu.Lock()
	f.grabRequests = append(f.grabRequests, request)
	barrier := f.grabBarrier
	err := f.grabErr
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	return err
}

func (f *fakeRadarrClient) SetMonitored(_ context.Context, movieID int, monitored bool) (integrationradarr.Movie, error) {
	f.concurrentMu.Lock()
	f.monitorRequests = append(f.monitorRequests, monitoredRequest{movieID: movieID, monitored: monitored})
	if f.monitorErr != nil {
		f.concurrentMu.Unlock()
		return integrationradarr.Movie{}, f.monitorErr
	}
	movie, ok := f.movies[movieID]
	if !ok {
		f.concurrentMu.Unlock()
		return integrationradarr.Movie{}, integrationradarr.ErrNotFound
	}
	movie.Monitored = monitored
	f.movies[movieID] = movie
	hook := f.onSetMonitored
	f.concurrentMu.Unlock()
	if hook != nil {
		hook(movieID, monitored)
	}
	return movie, nil
}

func (f *fakeRadarrClient) StartMoviesSearch(_ context.Context, movieID int) (integrationradarr.Command, error) {
	f.concurrentMu.Lock()
	f.automaticSearches = append(f.automaticSearches, movieID)
	command, err := f.searchCommand, f.searchCommandErr
	if err == nil || f.searchLandsOnError {
		f.movieSearchCommands[movieID] = append(f.movieSearchCommands[movieID], command)
	}
	barrier := f.searchBarrier
	f.concurrentMu.Unlock()
	if barrier != nil {
		barrier.arrived <- struct{}{}
		<-barrier.release
	}
	return command, err
}

func (f *fakeRadarrClient) FindRecentMoviesSearchCommand(
	_ context.Context,
	movieID int,
	queuedAfter time.Time,
) (*integrationradarr.Command, error) {
	f.concurrentMu.Lock()
	defer f.concurrentMu.Unlock()
	f.movieSearchLookups = append(f.movieSearchLookups, movieID)
	if f.movieSearchLookupErr != nil {
		return nil, f.movieSearchLookupErr
	}
	var found *integrationradarr.Command
	for _, command := range f.movieSearchCommands[movieID] {
		if command.Queued.Before(queuedAfter) || found != nil && !command.Queued.After(found.Queued) {
			continue
		}
		copy := command
		found = &copy
	}
	return found, nil
}

func (f *fakeRadarrClient) GetCommand(_ context.Context, id int) (integrationradarr.Command, error) {
	if err := f.commandErrors[id]; err != nil {
		return integrationradarr.Command{}, err
	}
	command, ok := f.commands[id]
	if !ok {
		return integrationradarr.Command{}, integrationradarr.ErrNotFound
	}
	return command, nil
}

type radarrAcquisitionServiceTestEnv struct {
	ctx           context.Context
	pool          *db.Pool
	repo          *repository.SqliteRadarrRepository
	service       *radarrService
	clients       map[string]*fakeRadarrClient
	primary       *fakeRadarrClient
	instance      repository.RadarrInstance
	preset        repository.RadarrPreset
	acquisitionID int64
	actorID       int
	now           time.Time
}

func setupRadarrAcquisitionServiceTest(
	t *testing.T,
	mode string,
	primary *fakeRadarrClient,
) *radarrAcquisitionServiceTestEnv {
	t.Helper()
	ctx := context.Background()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "radarr-acquisition.db"))
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		_ = pool.Close()
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("close SQLite: %v", err)
		}
	})

	users := repository.NewSqliteUserRepository(pool)
	movies := repository.NewSqliteMoviesRepository(pool)
	repo := repository.NewSqliteRadarrRepository(pool)
	actor, err := users.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create Admin: %v", err)
	}
	movie, err := movies.Add(ctx, "Heat", "pool", actor.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}
	tmdbID := 949
	if err := movies.SetExternalIDs(ctx, movie.ID, &tmdbID, nil); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	if err := movies.StartDraw(ctx, movie.ID, now, now.Add(16*time.Second), "drawer"); err != nil {
		t.Fatalf("start draw: %v", err)
	}
	if err := movies.RevealDraw(ctx, movie.ID, now.Add(17*time.Second)); err != nil {
		t.Fatalf("reveal draw: %v", err)
	}
	var acquisitionID int64
	if err := pool.Read.QueryRowContext(ctx,
		"SELECT id FROM radarr_acquisitions WHERE movie_id = ?", movie.ID,
	).Scan(&acquisitionID); err != nil {
		t.Fatalf("read Acquisition ID: %v", err)
	}

	if primary == nil {
		primary = newFakeRadarrClient()
	}
	instanceURL := "http://radarr-primary.test"
	instance, err := repo.CreateInstance(ctx, repository.RadarrInstanceSave{
		Name: "Primary", BaseURL: instanceURL, EncryptedAPIKey: []byte("primary-key"),
		State: radarrInstanceConnected, CheckedAt: now,
	})
	if err != nil {
		t.Fatalf("create Radarr instance: %v", err)
	}
	preset, err := repo.CreatePreset(ctx, repository.RadarrPresetSave{
		Name: "Living room", InstanceID: instance.ID,
		RootFolderID: 10, RootFolderPath: "/media/movies",
		QualityProfileID: 20, QualityProfileName: "HD-1080p",
		Tags:                []repository.RadarrTagSnapshot{{ID: 30, Label: "movies"}},
		MinimumAvailability: "released", AcquisitionMode: mode,
		Valid: true, ValidatedAt: now,
	})
	if err != nil {
		t.Fatalf("create Radarr preset: %v", err)
	}
	clients := map[string]*fakeRadarrClient{instanceURL: primary}
	service := newRadarrService(
		repo,
		passthroughRadarrSecrets{},
		func(baseURL, _ string) (integrationradarr.Client, error) {
			client, ok := clients[baseURL]
			if !ok {
				return nil, errors.New("unexpected Radarr instance")
			}
			return client, nil
		},
		"",
	)
	service.now = func() time.Time { return now.Add(time.Minute) }
	return &radarrAcquisitionServiceTestEnv{
		ctx: ctx, pool: pool, repo: repo, service: service, clients: clients,
		primary: primary, instance: instance, preset: preset,
		acquisitionID: acquisitionID, actorID: actor.ID, now: now,
	}
}

func (e *radarrAcquisitionServiceTestEnv) selectPreset(t *testing.T) repository.RadarrAcquisition {
	t.Helper()
	acquisition, err := e.service.selectPreset(e.ctx, e.acquisitionID, e.preset.ID, e.actorID)
	if err != nil {
		t.Fatalf("select Acquisition preset: %v", err)
	}
	return acquisition
}

func (e *radarrAcquisitionServiceTestEnv) selectPresetWithoutPreview(t *testing.T) repository.RadarrAcquisition {
	t.Helper()
	acquisition, err := e.repo.SelectAcquisitionPreset(
		e.ctx, e.acquisitionID, e.preset.ID, e.actorID, e.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("snapshot Acquisition preset: %v", err)
	}
	return acquisition
}

func TestConfirmAcquisitionRequiresPreviewAndAutoAdoptsChangedExistingTarget(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 41, TMDBID: 949, Title: "Heat", RootFolderPath: "/existing/movies",
		QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced, Monitored: true,
	}
	client.findMovie = &remote
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	selected := env.selectPresetWithoutPreview(t)

	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("confirm without Target review = %v, want conflict", err)
	}
	if len(client.addRequests) != 0 {
		t.Fatalf("confirm without Target review sent %d adds", len(client.addRequests))
	}

	observation, err := env.service.previewAcquisitionTargetObservation(env.ctx, selected)
	if err != nil {
		t.Fatalf("preview Acquisition target: %v", err)
	}
	previewed := observation.acquisition
	if previewed.TargetPreviewedAt == nil || previewed.EffectiveConfiguration.RootFolderPath != "/existing/movies" {
		t.Fatalf("Target review = %+v", previewed)
	}
	remote.RootFolderPath = "/existing/changed"
	client.findMovie = &remote
	confirmed, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("adopt changed Existing target: %v", err)
	}
	if !confirmed.TargetLocked() || !confirmed.AdoptedExisting ||
		confirmed.EffectiveConfiguration.RootFolderPath != "/existing/changed" {
		t.Fatalf("adopted changed Existing target = %+v", confirmed)
	}
	if len(client.addRequests) != 0 {
		t.Fatalf("Existing adoption sent %d adds", len(client.addRequests))
	}
}

func TestConcurrentConfirmDoesNotResetLiveAddClaim(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 159, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.addBarrier = barrier
	client.concurrentMu.Unlock()
	released := false
	release := func() {
		if !released {
			close(barrier.release)
			released = true
		}
	}
	defer release()

	type confirmResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	firstDone := make(chan confirmResult, 1)
	go func() {
		acquisition, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
		firstDone <- confirmResult{acquisition: acquisition, err: err}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("first confirmation did not reach AddMovie")
	}

	current, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("observe concurrent confirmation: %v", err)
	}
	if current.MutationState != "adding" || current.TargetLocked() || current.NextCheckAt == nil ||
		!current.NextCheckAt.After(env.service.now()) {
		t.Fatalf("concurrent confirmation state = %+v", current)
	}
	client.concurrentMu.Lock()
	findCalls := client.findCalls
	addRequests := len(client.addRequests)
	client.concurrentMu.Unlock()
	if findCalls != 2 || addRequests != 1 {
		t.Fatalf("concurrent confirmation made remote calls: find=%d add=%d", findCalls, addRequests)
	}

	release()
	select {
	case result := <-firstDone:
		if result.err != nil {
			t.Fatalf("first confirmation: %v", result.err)
		}
		if !result.acquisition.TargetLocked() || result.acquisition.RadarrMovieID == nil ||
			*result.acquisition.RadarrMovieID != 159 {
			t.Fatalf("first confirmation result = %+v", result.acquisition)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first confirmation did not finish")
	}
	if len(client.addRequests) != 1 {
		t.Fatalf("AddMovie calls = %d, want 1", len(client.addRequests))
	}
}

func TestAmbiguousAddResponseIsImmediatelyReconciledWithoutDuplicate(t *testing.T) {
	client := newFakeRadarrClient()
	client.addErr = integrationradarr.ErrTransient
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)

	ambiguous, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm with ambiguous AddMovie response: %v", err)
	}
	if ambiguous.MutationState != "adding" || ambiguous.ActionReason != "connection_failed" ||
		ambiguous.NextCheckAt == nil || !ambiguous.NextCheckAt.After(env.now) {
		t.Fatalf("ambiguous AddMovie state = %+v", ambiguous)
	}

	landed := integrationradarr.Movie{ID: 160, TMDBID: 949, Title: "Heat", Monitored: false}
	client.addErr = nil
	client.findMovie = &landed
	client.movies[160] = landed
	client.concurrentMu.Lock()
	findCallsBeforeLeaseRetry := client.findCalls
	client.concurrentMu.Unlock()
	leased, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("retry during unknown add lease: %v", err)
	}
	client.concurrentMu.Lock()
	findCallsDuringLease := client.findCalls
	client.concurrentMu.Unlock()
	if leased.MutationState != "adding" || leased.TargetLocked() ||
		findCallsDuringLease != findCallsBeforeLeaseRetry {
		t.Fatalf("retry bypassed unknown add lease: acquisition=%+v find calls=%d -> %d",
			leased, findCallsBeforeLeaseRetry, findCallsDuringLease)
	}
	leaseExpiredAt := ambiguous.NextCheckAt.Add(time.Second)
	env.service.now = func() time.Time { return leaseExpiredAt }
	recovered, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("reconcile ambiguous AddMovie: %v", err)
	}
	if !recovered.TargetLocked() || recovered.RadarrMovieID == nil || *recovered.RadarrMovieID != 160 ||
		recovered.AdoptedExisting {
		t.Fatalf("recovered AddMovie = %+v", recovered)
	}
	if len(client.addRequests) != 1 {
		t.Fatalf("AddMovie calls = %d, want 1", len(client.addRequests))
	}
}

func TestRetryUnlockedAcquisitionOnlyRefreshesTargetReview(t *testing.T) {
	env := setupRadarrAcquisitionServiceTest(t, "manual", newFakeRadarrClient())
	env.selectPresetWithoutPreview(t)

	for range 2 {
		acquisition, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
		if err != nil {
			t.Fatalf("retry unlocked Acquisition: %v", err)
		}
		if acquisition.TargetPreviewedAt == nil || acquisition.TargetLocked() {
			t.Fatalf("retry result = %+v", acquisition)
		}
	}
	if len(env.primary.addRequests) != 0 || len(env.primary.automaticSearches) != 0 || len(env.primary.grabRequests) != 0 {
		t.Fatalf("unlocked retry mutated Radarr: adds=%d searches=%d grabs=%d",
			len(env.primary.addRequests), len(env.primary.automaticSearches), len(env.primary.grabRequests))
	}
}

func TestRetryUnlockedAcquisitionAutoAdoptsExactExistingMovie(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 170, TMDBID: 949, Title: "Heat", Monitored: false,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPresetWithoutPreview(t)

	acquisition, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("retry Existing unlocked Acquisition: %v", err)
	}
	if acquisition.Status != "needs_release" || !acquisition.AdoptedExisting ||
		acquisition.RadarrMovieID == nil || *acquisition.RadarrMovieID != 170 {
		t.Fatalf("retried Existing Acquisition = %+v", acquisition)
	}
	if len(client.addRequests) != 0 || len(client.monitorRequests) != 0 {
		t.Fatalf("retried Existing movie was changed: adds=%v monitor=%v",
			client.addRequests, client.monitorRequests)
	}
}

func TestExistingRadarrMovieWithFileCompletesWithoutChangingConfiguration(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 42, TMDBID: 949, Title: "Heat", HasFile: true, Monitored: true,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	acquisition := env.selectPreset(t)

	if acquisition.Status != "downloaded" || !acquisition.AdoptedExisting || acquisition.RadarrMovieID == nil || *acquisition.RadarrMovieID != 42 {
		t.Fatalf("completed Existing Acquisition = %+v", acquisition)
	}
	if acquisition.TargetRootFolderPath != "/media/movies" || acquisition.TargetQualityProfileName != "HD-1080p" {
		t.Fatalf("preset snapshot changed = %+v", acquisition)
	}
	if acquisition.EffectiveConfiguration.RootFolderPath != "/existing/movies" ||
		acquisition.EffectiveConfiguration.QualityProfileName != "Legacy profile" ||
		!acquisition.EffectiveConfiguration.Monitored ||
		len(acquisition.EffectiveConfiguration.Tags) != 1 || acquisition.EffectiveConfiguration.Tags[0].Label != "legacy" {
		t.Fatalf("preserved effective configuration = %+v", acquisition.EffectiveConfiguration)
	}
	if len(client.addRequests) != 0 || len(client.monitorRequests) != 0 || len(client.automaticSearches) != 0 || len(client.queueCalls) != 0 {
		t.Fatalf("Existing completed movie was mutated: adds=%d monitor=%d search=%d queue=%d",
			len(client.addRequests), len(client.monitorRequests), len(client.automaticSearches), len(client.queueCalls))
	}
}

func TestExistingRadarrMovieDiscoveredDuringConfirmAutoAdopts(t *testing.T) {
	client := newFakeRadarrClient()
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	previewed := env.selectPreset(t)
	if previewed.TargetLocked() || previewed.TargetPreviewExisting {
		t.Fatalf("initial missing-movie preview = %+v", previewed)
	}
	remote := integrationradarr.Movie{
		ID: 168, TMDBID: 949, Title: "Heat", HasFile: true, Monitored: true,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote

	acquisition, err := env.service.confirmAcquisitionTarget(
		env.ctx, env.acquisitionID, env.actorID,
	)
	if err != nil {
		t.Fatalf("adopt movie discovered during confirmation: %v", err)
	}
	if acquisition.Status != "downloaded" || !acquisition.AdoptedExisting ||
		acquisition.RadarrMovieID == nil || *acquisition.RadarrMovieID != 168 {
		t.Fatalf("adopted Existing Acquisition = %+v", acquisition)
	}
	if len(client.addRequests) != 0 {
		t.Fatalf("Existing movie triggered add: %+v", client.addRequests)
	}
}

func TestExistingRadarrMovieAutoAdoptsAfterIdentitySelection(t *testing.T) {
	client := newFakeRadarrClient()
	client.lookupMovie = integrationradarr.MovieCandidate{TMDBID: 949, Title: "Heat"}
	remote := integrationradarr.Movie{
		ID: 169, TMDBID: 949, Title: "Heat", HasFile: true, Monitored: true,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	if _, err := env.pool.Write.ExecContext(
		env.ctx, "UPDATE radarr_acquisitions SET tmdb_id = NULL WHERE id = ?", env.acquisitionID,
	); err != nil {
		t.Fatalf("clear Acquisition identity: %v", err)
	}
	selected := env.selectPreset(t)
	if selected.ActionReason != "identity_required" || selected.TargetLocked() {
		t.Fatalf("identity-required Acquisition = %+v", selected)
	}

	acquisition, err := env.service.selectAcquisitionIdentity(
		env.ctx, env.acquisitionID, 949, env.actorID,
	)
	if err != nil {
		t.Fatalf("select Existing movie identity: %v", err)
	}
	if acquisition.Status != "downloaded" || !acquisition.AdoptedExisting ||
		acquisition.RadarrMovieID == nil || *acquisition.RadarrMovieID != 169 ||
		acquisition.TargetLockedBy == nil || *acquisition.TargetLockedBy != env.actorID {
		t.Fatalf("identity-selected Existing Acquisition = %+v", acquisition)
	}
	if len(client.addRequests) != 0 {
		t.Fatalf("identity-selected Existing movie triggered add: %+v", client.addRequests)
	}
}

func TestExistingRadarrMovieWithoutFileAutoAdoptsAndFollowsAutomaticMode(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 167, TMDBID: 949, Title: "Heat", Monitored: false,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote
	client.movies[167] = remote
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)

	acquisition := env.selectPreset(t)

	if acquisition.Status != "waiting_for_radarr" || !acquisition.AdoptedExisting ||
		acquisition.RadarrMovieID == nil || *acquisition.RadarrMovieID != 167 ||
		acquisition.AutomaticSearchCommandID == nil || *acquisition.AutomaticSearchCommandID != 700 {
		t.Fatalf("adopted Existing Automatic Acquisition = %+v", acquisition)
	}
	if acquisition.EffectiveConfiguration.Monitored {
		t.Fatalf("Existing movie monitoring changed: %+v", acquisition.EffectiveConfiguration)
	}
	if len(client.addRequests) != 0 || len(client.monitorRequests) != 0 ||
		len(client.automaticSearches) != 1 || client.automaticSearches[0] != 167 {
		t.Fatalf("Existing Automatic work: adds=%v monitor=%v searches=%v",
			client.addRequests, client.monitorRequests, client.automaticSearches)
	}
}

func TestManualAcquisitionAddsUnmonitoredThenMonitorsAfterReleaseGrab(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 43, TMDBID: 949, Title: "Heat"}}
	client.releases[43] = []integrationradarr.Release{{
		ID: "opaque-release", Title: "Heat.1995.1080p", Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)

	added, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	if added.Status != "needs_release" || added.ActionReason != "release_required" {
		t.Fatalf("Manual add state = %+v", added)
	}
	if len(client.addRequests) != 1 || client.addRequests[0].Mode != integrationradarr.AcquisitionModeManual || client.movies[43].Monitored {
		t.Fatalf("Manual add request = %+v, remote = %+v", client.addRequests, client.movies[43])
	}
	if len(client.automaticSearches) != 0 || len(client.monitorRequests) != 0 {
		t.Fatalf("Manual add started work early: searches=%v monitor=%v", client.automaticSearches, client.monitorRequests)
	}

	releases, err := env.service.searchReleases(env.ctx, env.acquisitionID)
	if err != nil || len(releases) != 1 {
		t.Fatalf("Interactive search = %+v, err %v", releases, err)
	}
	queued, err := env.service.grabRelease(env.ctx, env.acquisitionID, "opaque-release", false, env.actorID)
	if err != nil {
		t.Fatalf("grab selected release: %v", err)
	}
	if queued.Status != "queued" || queued.ManualAttemptCount != 1 || queued.LatestReleaseTitle != "Heat.1995.1080p" {
		t.Fatalf("queued Manual Acquisition = %+v", queued)
	}
	if len(client.grabRequests) != 1 || client.grabRequests[0].ResultID != "opaque-release" {
		t.Fatalf("grab requests = %+v", client.grabRequests)
	}
	if len(client.monitorRequests) != 1 || client.monitorRequests[0] != (monitoredRequest{movieID: 43, monitored: true}) || !client.movies[43].Monitored {
		t.Fatalf("monitor requests = %+v, remote = %+v", client.monitorRequests, client.movies[43])
	}
}

func TestRetryAmbiguousManualGrabObservesRadarrWithoutSecondGrab(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 45, TMDBID: 949, Title: "Heat"}}
	client.releases[45] = []integrationradarr.Release{{
		ID: "ambiguous-release", Title: "Heat.1995.1080p",
		Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
		t.Fatalf("search Manual releases: %v", err)
	}
	client.grabErr = integrationradarr.ErrTransient
	if _, err := env.service.grabRelease(
		env.ctx, env.acquisitionID, "ambiguous-release", false, env.actorID,
	); !errors.Is(err, integrationradarr.ErrTransient) {
		t.Fatalf("ambiguous release grab = %v, want transient error", err)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read ambiguous release grab: %v", err)
	}
	if stored.MutationState != "grabbing" || stored.ActionReason != "connection_failed" {
		t.Fatalf("ambiguous release state = %+v", stored)
	}
	client.queue[45] = []integrationradarr.QueueItem{{
		ID: 8, MovieID: 45, Title: "Heat.1995.1080p",
		Status: "downloading", TrackedDownloadState: "downloading",
	}}
	client.grabErr = nil

	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("check ambiguous release grab: %v", err)
	}
	if recovered.Status != "downloading" || recovered.MutationState != "idle" || !client.movies[45].Monitored {
		t.Fatalf("recovered release grab = %+v, movie=%+v", recovered, client.movies[45])
	}
	if len(client.grabRequests) != 1 || len(client.monitorRequests) != 1 {
		t.Fatalf("ambiguous recovery mutations: grabs=%+v monitor=%+v", client.grabRequests, client.monitorRequests)
	}
}

func TestLateManualGrabFinalizationDoesNotOverwriteWorkerObservation(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 46, TMDBID: 949, Title: "Heat"}}
	client.releases[46] = []integrationradarr.Release{{
		ID: "slow-release", Title: "Heat.1995.1080p",
		Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
		t.Fatalf("search Manual releases: %v", err)
	}
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.grabBarrier = barrier
	client.concurrentMu.Unlock()
	released := false
	release := func() {
		if !released {
			close(barrier.release)
			released = true
		}
	}
	defer release()

	type grabResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	done := make(chan grabResult, 1)
	go func() {
		acquisition, err := env.service.grabRelease(
			env.ctx, env.acquisitionID, "slow-release", false, env.actorID,
		)
		done <- grabResult{acquisition: acquisition, err: err}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("manual grab did not reach Radarr")
	}
	claimed, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read in-flight manual grab: %v", err)
	}
	if claimed.MutationState != "grabbing" || claimed.NextCheckAt == nil ||
		!claimed.NextCheckAt.After(env.service.now()) {
		t.Fatalf("manual grab claim = %+v", claimed)
	}
	client.queue[46] = []integrationradarr.QueueItem{{
		ID: 9, MovieID: 46, Title: "Heat.1995.1080p",
		Status: "downloading", TrackedDownloadState: "downloading",
	}}
	observed, err := env.service.reconcileOne(env.ctx, claimed)
	if err != nil {
		t.Fatalf("observe in-flight manual grab: %v", err)
	}
	if observed.Status != "downloading" || observed.MutationState != "idle" {
		t.Fatalf("worker observation = %+v", observed)
	}
	release()
	result := <-done
	if result.err != nil {
		t.Fatalf("complete slow manual grab: %v", result.err)
	}
	if result.acquisition.Status != "downloading" || result.acquisition.Revision != observed.Revision {
		t.Fatalf("late manual grab finalization = %+v, want observation %+v", result.acquisition, observed)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read final manual grab state: %v", err)
	}
	if stored.Status != "downloading" || stored.Revision != observed.Revision {
		t.Fatalf("late manual grab overwrote worker observation: %+v", stored)
	}
	if len(client.grabRequests) != 1 {
		t.Fatalf("slow manual grab requests = %+v", client.grabRequests)
	}
}

func TestOversizedManualGrabResponseRemainsClaimed(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 47, TMDBID: 949, Title: "Heat"}}
	client.releases[47] = []integrationradarr.Release{{
		ID: "oversized-release", Title: "Heat.1995.1080p",
		Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
		t.Fatalf("search Manual releases: %v", err)
	}
	client.grabErr = integrationradarr.ErrResponseTooLarge
	if _, err := env.service.grabRelease(
		env.ctx, env.acquisitionID, "oversized-release", false, env.actorID,
	); !errors.Is(err, integrationradarr.ErrResponseTooLarge) {
		t.Fatalf("oversized release response = %v, want response-too-large error", err)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read oversized release response: %v", err)
	}
	if stored.MutationState != "grabbing" || stored.ActionReason != "connection_failed" ||
		stored.NextCheckAt == nil || !stored.NextCheckAt.After(env.service.now()) {
		t.Fatalf("oversized release state = %+v", stored)
	}
	client.grabErr = nil
	if _, err := env.service.grabRelease(
		env.ctx, env.acquisitionID, "oversized-release", false, env.actorID,
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("repeat grab while outcome is unknown = %v, want conflict", err)
	}
	if len(client.grabRequests) != 1 {
		t.Fatalf("oversized response permitted a second grab: %+v", client.grabRequests)
	}
}

func TestAutomaticAcquisitionStartsOneRadarrSearch(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 44, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	env.selectPreset(t)

	acquisition, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	if acquisition.Status != "waiting_for_radarr" || acquisition.AutomaticSearchCommandID == nil || *acquisition.AutomaticSearchCommandID != 700 {
		t.Fatalf("Automatic Acquisition = %+v", acquisition)
	}
	if len(client.addRequests) != 1 || client.addRequests[0].Mode != integrationradarr.AcquisitionModeAutomatic {
		t.Fatalf("Automatic add requests = %+v", client.addRequests)
	}
	if len(client.automaticSearches) != 1 || client.automaticSearches[0] != 44 || len(client.monitorRequests) != 0 {
		t.Fatalf("Automatic commands = %+v, monitor = %+v", client.automaticSearches, client.monitorRequests)
	}
}

func TestConcurrentAutomaticRetryStartsOneMoviesSearch(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 161, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	if locked.AutomaticSearchCommandID == nil {
		t.Fatalf("Automatic Acquisition has no command: %+v", locked)
	}
	if err := env.repo.CompleteAutomaticSearch(
		env.ctx, locked.ID, locked.Revision, *locked.AutomaticSearchCommandID, env.service.now(),
	); err != nil {
		t.Fatalf("complete initial Automatic search: %v", err)
	}
	completed, err := env.repo.GetVisibleAcquisition(env.ctx, locked.ID)
	if err != nil {
		t.Fatalf("read completed Automatic search: %v", err)
	}
	retryable, err := env.repo.TransitionAcquisition(env.ctx, completed.ID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "no_releases",
		QueueStatus: "none", At: env.service.now(),
	})
	if err != nil {
		t.Fatalf("make Automatic Acquisition retryable: %v", err)
	}
	client.searchCommand = integrationradarr.Command{
		ID: 701, Name: "MoviesSearch", Status: "queued", Queued: env.service.now(),
	}
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.searchBarrier = barrier
	client.concurrentMu.Unlock()
	released := false
	release := func() {
		if !released {
			close(barrier.release)
			released = true
		}
	}
	defer release()

	type retryResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	firstDone := make(chan retryResult, 1)
	secondDone := make(chan retryResult, 1)
	go func() {
		acquisition, retryErr := env.service.retryAcquisition(env.ctx, retryable.ID, env.actorID)
		firstDone <- retryResult{acquisition: acquisition, err: retryErr}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("first Retry did not reach MoviesSearch")
	}
	go func() {
		acquisition, retryErr := env.service.retryAcquisition(env.ctx, retryable.ID, env.actorID)
		secondDone <- retryResult{acquisition: acquisition, err: retryErr}
	}()

	var second retryResult
	duplicatePost := false
	select {
	case second = <-secondDone:
	case <-barrier.arrived:
		duplicatePost = true
	}
	release()
	first := <-firstDone
	if duplicatePost {
		second = <-secondDone
	}
	if first.err != nil {
		t.Fatalf("first Automatic Retry: %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second Automatic Retry: %v", second.err)
	}
	if duplicatePost {
		t.Fatal("concurrent Retry sent a second MoviesSearch")
	}
	if second.acquisition.MutationState != "searching" || second.acquisition.NextCheckAt == nil {
		t.Fatalf("second Retry did not observe search claim: %+v", second.acquisition)
	}
	if first.acquisition.AutomaticSearchCommandID == nil || *first.acquisition.AutomaticSearchCommandID != 701 ||
		first.acquisition.MutationState != "idle" {
		t.Fatalf("first Automatic Retry result = %+v", first.acquisition)
	}
	client.concurrentMu.Lock()
	searches := append([]int(nil), client.automaticSearches...)
	client.concurrentMu.Unlock()
	if len(searches) != 2 || searches[0] != 161 || searches[1] != 161 {
		t.Fatalf("MoviesSearch calls = %v, want initial plus one Retry", searches)
	}
}

func TestMissingStoredAutomaticSearchCommandAdoptsRecentCommandWithoutResend(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 165, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	client.searchCommand = integrationradarr.Command{
		ID: 704, Name: "MoviesSearch", Status: "queued", Queued: env.service.now(),
	}
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	retryable, err := env.repo.TransitionAcquisition(env.ctx, locked.ID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "connection_failed",
		FailureSummary: radarrAutomaticSearchUnknownReason,
		QueueStatus:    "none", At: env.service.now(),
	})
	if err != nil {
		t.Fatalf("make stored command retryable: %v", err)
	}

	recovered, err := env.service.retryAcquisition(env.ctx, retryable.ID, env.actorID)
	if err != nil {
		t.Fatalf("check missing stored command: %v", err)
	}
	if recovered.Status != "waiting_for_radarr" || recovered.ActionReason != "" ||
		recovered.AutomaticSearchCommandID == nil || *recovered.AutomaticSearchCommandID != 704 ||
		recovered.AutomaticSearchCompletedAt != nil {
		t.Fatalf("recovered stored command = %+v", recovered)
	}
	client.concurrentMu.Lock()
	searchCount := len(client.automaticSearches)
	lookupCount := len(client.movieSearchLookups)
	client.concurrentMu.Unlock()
	if searchCount != 1 || lookupCount != 1 {
		t.Fatalf("stored command recovery calls: searches=%d command lookups=%d", searchCount, lookupCount)
	}
}

func TestMissingAutomaticSearchCommandStaysUnknownWithoutResend(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 166, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	client.searchCommand = integrationradarr.Command{
		ID: 705, Name: "MoviesSearch", Status: "queued", Queued: env.service.now(),
	}
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	client.concurrentMu.Lock()
	client.movieSearchCommands[166] = nil
	client.concurrentMu.Unlock()
	retryable, err := env.repo.TransitionAcquisition(env.ctx, locked.ID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "connection_failed",
		FailureSummary: radarrAutomaticSearchUnknownReason,
		QueueStatus:    "none", At: env.service.now(),
	})
	if err != nil {
		t.Fatalf("make missing command retryable: %v", err)
	}

	for range 2 {
		retryable, err = env.service.retryAcquisition(env.ctx, retryable.ID, env.actorID)
		if err != nil {
			t.Fatalf("check unknown Automatic search: %v", err)
		}
	}
	if retryable.Status != "action_needed" || retryable.ActionReason != "connection_failed" ||
		retryable.AutomaticSearchCommandID == nil || *retryable.AutomaticSearchCommandID != 705 ||
		retryable.AutomaticSearchClaimedAt == nil || retryable.AutomaticSearchCompletedAt != nil {
		t.Fatalf("unknown stored command = %+v", retryable)
	}
	client.concurrentMu.Lock()
	searchCount := len(client.automaticSearches)
	lookupCount := len(client.movieSearchLookups)
	client.concurrentMu.Unlock()
	if searchCount != 1 || lookupCount != 2 {
		t.Fatalf("unknown command checks: searches=%d command lookups=%d", searchCount, lookupCount)
	}
}

func TestAmbiguousMoviesSearchResponseAdoptsRemoteCommandWithoutResend(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 162, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	client.searchCommand = integrationradarr.Command{
		ID: 702, Name: "MoviesSearch", Status: "queued", Queued: env.service.now(),
	}
	client.searchCommandErr = integrationradarr.ErrTransient
	client.searchLandsOnError = true
	env.selectPreset(t)

	ambiguous, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm with ambiguous MoviesSearch response: %v", err)
	}
	if ambiguous.ActionReason != "connection_failed" {
		t.Fatalf("ambiguous MoviesSearch state = %+v", ambiguous)
	}
	client.searchCommandErr = nil
	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("recover ambiguous MoviesSearch: %v", err)
	}
	if recovered.AutomaticSearchCommandID == nil || *recovered.AutomaticSearchCommandID != 702 ||
		recovered.MutationState != "idle" || recovered.Status != "waiting_for_radarr" {
		t.Fatalf("recovered MoviesSearch = %+v", recovered)
	}
	client.concurrentMu.Lock()
	searchCount := len(client.automaticSearches)
	lookupCount := len(client.movieSearchLookups)
	client.concurrentMu.Unlock()
	if searchCount != 1 || lookupCount != 1 {
		t.Fatalf("ambiguous recovery calls: searches=%d command lookups=%d", searchCount, lookupCount)
	}
}

func TestMoviesSearchCommandRecordFailureAdoptsRemoteCommandWithoutResend(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 163, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	client.searchCommand = integrationradarr.Command{
		ID: 703, Name: "MoviesSearch", Status: "queued", Queued: env.service.now(),
	}
	env.selectPreset(t)
	if _, err := env.pool.Write.ExecContext(env.ctx, `
		CREATE TRIGGER fail_radarr_search_command_record
		BEFORE UPDATE OF automatic_search_command_id ON radarr_acquisitions
		WHEN NEW.automatic_search_command_id IS NOT NULL
		BEGIN
			SELECT RAISE(FAIL, 'forced command record failure');
		END
	`); err != nil {
		t.Fatalf("create command-record failure trigger: %v", err)
	}
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err == nil {
		t.Fatal("confirm with command-record failure succeeded")
	}
	if _, err := env.pool.Write.ExecContext(env.ctx, `DROP TRIGGER fail_radarr_search_command_record`); err != nil {
		t.Fatalf("drop command-record failure trigger: %v", err)
	}
	if _, err := env.repo.TransitionAcquisition(env.ctx, env.acquisitionID, repository.RadarrAcquisitionTransition{
		Status: "action_needed", ActionReason: "release_failed",
		FailureSummary: "Search command record failed.", QueueStatus: "none", At: env.now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatalf("expire command-record claim: %v", err)
	}
	env.service.now = func() time.Time { return env.now.Add(2 * time.Minute) }

	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("recover unrecorded MoviesSearch: %v", err)
	}
	if recovered.AutomaticSearchCommandID == nil || *recovered.AutomaticSearchCommandID != 703 ||
		recovered.MutationState != "idle" {
		t.Fatalf("recovered unrecorded MoviesSearch = %+v", recovered)
	}
	client.concurrentMu.Lock()
	searchCount := len(client.automaticSearches)
	lookupCount := len(client.movieSearchLookups)
	client.concurrentMu.Unlock()
	if searchCount != 1 || lookupCount != 1 {
		t.Fatalf("command-record recovery calls: searches=%d command lookups=%d", searchCount, lookupCount)
	}
}

func TestUnknownAutomaticSearchCanBeAbandonedAfterWarning(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 164, TMDBID: 949, Title: "Heat"}}
	client.searchCommandErr = integrationradarr.ErrTransient
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	env.selectPreset(t)

	unknown, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm with unknown automatic search: %v", err)
	}
	if unknown.MutationState != "searching" || unknown.ActionReason != "connection_failed" {
		t.Fatalf("unknown automatic search = %+v", unknown)
	}

	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review unknown automatic search: %v", err)
	}
	if review.Activity != "unavailable" || review.Acquisition.MutationState != "searching" {
		t.Fatalf("unknown automatic search review = %+v", review)
	}
	abandoned, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "Search outcome cannot be confirmed.", "unavailable",
	)
	if err != nil {
		t.Fatalf("abandon unknown automatic search: %v", err)
	}
	if abandoned.Status != "abandoned" || abandoned.MutationState != "idle" ||
		abandoned.AutomaticSearchClaimedAt != nil {
		t.Fatalf("abandoned unknown automatic search = %+v", abandoned)
	}
}

func TestAmbiguousPreLockAddCanBeAbandonedAfterWarning(t *testing.T) {
	client := newFakeRadarrClient()
	client.addErr = integrationradarr.ErrTransient
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)

	ambiguous, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm with unknown add result: %v", err)
	}
	if ambiguous.MutationState != "adding" || ambiguous.TargetLocked() ||
		ambiguous.ActionReason != "connection_failed" {
		t.Fatalf("unknown add result = %+v", ambiguous)
	}

	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review unknown add result: %v", err)
	}
	if review.Activity != "unavailable" || review.Acquisition.MutationState != "adding" {
		t.Fatalf("unknown add review = %+v", review)
	}
	abandoned, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "The add outcome cannot be confirmed.", "unavailable",
	)
	if err != nil {
		t.Fatalf("abandon unknown add result: %v", err)
	}
	if abandoned.Status != "abandoned" || abandoned.MutationState != "idle" {
		t.Fatalf("abandoned unknown add result = %+v", abandoned)
	}
}

func TestAutomaticSearchCompletionUsesRelevantHistoryBeforeFallback(t *testing.T) {
	tests := []struct {
		name          string
		commandStatus string
		history       []integrationradarr.HistoryItem
		wantReason    string
		wantSummary   string
	}{
		{
			name:          "completed search with failed import",
			commandStatus: "completed",
			history: []integrationradarr.HistoryItem{{
				EventType: "downloadFolderImported",
			}},
			wantReason:  "import_failed",
			wantSummary: "Radarr imported the download but no movie file is available.",
		},
		{
			name:          "completed search with failed download",
			commandStatus: "completed",
			history: []integrationradarr.HistoryItem{{
				EventType: "downloadFailed",
			}},
			wantReason:  "release_failed",
			wantSummary: "Radarr reports that the selected download failed.",
		},
		{
			name:          "failed search with failed import",
			commandStatus: "failed",
			history: []integrationradarr.HistoryItem{{
				EventType: "movieFolderImported",
			}},
			wantReason:  "import_failed",
			wantSummary: "Radarr imported the download but no movie file is available.",
		},
		{
			name:          "completed search ignores older failure",
			commandStatus: "completed",
			history: []integrationradarr.HistoryItem{{
				EventType: "downloadFailed",
			}},
			wantReason:  "no_releases",
			wantSummary: "The automatic search completed without a file or active queue item.",
		},
		{
			name:          "failed search without relevant failure",
			commandStatus: "failed",
			wantReason:    "release_failed",
			wantSummary:   "The automatic Radarr search failed.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeRadarrClient()
			client.addResults = []integrationradarr.Movie{{ID: 71, TMDBID: 949, Title: "Heat"}}
			env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
			searchStartedAt := env.now.Add(time.Minute)
			client.searchCommand.Queued = searchStartedAt
			env.selectPreset(t)
			locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
			if err != nil {
				t.Fatalf("confirm Automatic Acquisition: %v", err)
			}

			client.commands[700] = integrationradarr.Command{
				ID: 700, Name: "MoviesSearch", Status: test.commandStatus, Queued: searchStartedAt,
			}
			for i := range test.history {
				test.history[i].ID = i + 1
				test.history[i].MovieID = 71
				if test.name == "completed search ignores older failure" {
					test.history[i].Date = searchStartedAt.Add(-time.Second)
				} else {
					test.history[i].Date = searchStartedAt.Add(time.Second)
				}
			}
			client.history[71] = test.history

			reconciled, err := env.service.reconcileOne(env.ctx, locked)
			if err != nil {
				t.Fatalf("reconcile finished Automatic search: %v", err)
			}
			if reconciled.Status != "action_needed" || reconciled.ActionReason != test.wantReason ||
				reconciled.LatestFailureSummary != test.wantSummary {
				t.Fatalf("finished Automatic search = %+v", reconciled)
			}
			if len(client.historyCalls) != 1 || client.historyCalls[0] != 71 {
				t.Fatalf("history calls = %v, want selected movie 71", client.historyCalls)
			}

			rechecked, err := env.service.reconcileOne(env.ctx, reconciled)
			if err != nil {
				t.Fatalf("recheck finished Automatic search: %v", err)
			}
			if rechecked.Status != "action_needed" || rechecked.ActionReason != test.wantReason ||
				rechecked.LatestFailureSummary != test.wantSummary {
				t.Fatalf("rechecked Automatic search = %+v", rechecked)
			}
		})
	}
}

func TestReconciliationUsesOnlySelectedRadarrTarget(t *testing.T) {
	primary := newFakeRadarrClient()
	primary.addResults = []integrationradarr.Movie{{ID: 45, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", primary)
	secondary := newFakeRadarrClient()
	secondary.findMovie = &integrationradarr.Movie{ID: 900, TMDBID: 949, Title: "Heat", HasFile: true}
	secondary.movies[900] = *secondary.findMovie
	secondaryURL := "http://radarr-secondary.test"
	env.clients[secondaryURL] = secondary
	if _, err := env.repo.CreateInstance(env.ctx, repository.RadarrInstanceSave{
		Name: "Secondary", BaseURL: secondaryURL, EncryptedAPIKey: []byte("secondary-key"),
		State: radarrInstanceConnected, CheckedAt: env.now,
	}); err != nil {
		t.Fatalf("create secondary Radarr instance: %v", err)
	}
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm selected target: %v", err)
	}

	reconciled, err := env.service.reconcileOne(env.ctx, locked)
	if err != nil {
		t.Fatalf("reconcile selected target: %v", err)
	}
	if reconciled.Status != "needs_release" || reconciled.RadarrMovieID == nil || *reconciled.RadarrMovieID != 45 {
		t.Fatalf("selected-target reconciliation = %+v", reconciled)
	}
	if len(primary.getCalls) != 1 || primary.getCalls[0] != 45 {
		t.Fatalf("primary GetMovie calls = %+v", primary.getCalls)
	}
	if secondary.findCalls != 0 || len(secondary.getCalls) != 0 || len(secondary.queueCalls) != 0 {
		t.Fatalf("secondary target was queried: find=%d get=%v queue=%v", secondary.findCalls, secondary.getCalls, secondary.queueCalls)
	}
}

func TestImportBlockedQueueRequiresAdminAction(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 46, TMDBID: 949, Title: "Heat"}}
	client.queue[46] = []integrationradarr.QueueItem{{
		ID: 5, MovieID: 46, Title: "Heat.1995.1080p", TrackedDownloadState: "importBlocked",
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)

	acquisition, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Acquisition with import-blocked queue: %v", err)
	}
	if acquisition.Status != "action_needed" || acquisition.ActionReason != "import_failed" || acquisition.QueueStatus != "failed" {
		t.Fatalf("import-blocked Acquisition = %+v", acquisition)
	}
	if acquisition.LatestFailureSummary != "Radarr reports that the download is blocked from import." {
		t.Fatalf("import-blocked failure summary = %q", acquisition.LatestFailureSummary)
	}
}

func TestFailedQueueRequiresAdminAction(t *testing.T) {
	tests := []struct {
		name string
		item integrationradarr.QueueItem
	}{
		{name: "failed pending state", item: integrationradarr.QueueItem{TrackedDownloadState: "failedPending"}},
		{name: "failed state", item: integrationradarr.QueueItem{TrackedDownloadState: "failed"}},
		{name: "tracked error status", item: integrationradarr.QueueItem{TrackedDownloadStatus: "error"}},
		{name: "failed queue status", item: integrationradarr.QueueItem{Status: "failed"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeRadarrClient()
			client.addResults = []integrationradarr.Movie{{ID: 158, TMDBID: 949, Title: "Heat"}}
			test.item.ID = 9
			test.item.MovieID = 158
			test.item.Title = "Heat.1995.1080p"
			client.queue[158] = []integrationradarr.QueueItem{test.item}
			env := setupRadarrAcquisitionServiceTest(t, "manual", client)
			env.selectPreset(t)

			acquisition, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
			if err != nil {
				t.Fatalf("confirm Acquisition with failed queue: %v", err)
			}
			if acquisition.Status != "action_needed" || acquisition.ActionReason != "release_failed" ||
				acquisition.QueueStatus != "failed" {
				t.Fatalf("failed queue Acquisition = %+v", acquisition)
			}
			if acquisition.LatestFailureSummary != "Radarr reports that the selected download failed." {
				t.Fatalf("failed queue summary = %q", acquisition.LatestFailureSummary)
			}
		})
	}
}

func TestExistingActiveQueueIsObservedWithoutCompetingAutomaticSearch(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 47, TMDBID: 949, Title: "Heat", RootFolderPath: "/existing/movies",
		QualityProfileID: 91, TagIDs: []int{92}, MinimumAvailability: integrationradarr.AvailabilityReleased,
	}
	client.findMovie = &remote
	client.queue[47] = []integrationradarr.QueueItem{{
		ID: 6, MovieID: 47, Title: "Heat.1995.1080p", Status: "downloading", TrackedDownloadState: "downloading",
	}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	acquisition := env.selectPreset(t)

	if acquisition.Status != "downloading" || acquisition.QueueStatus != "downloading" || !acquisition.AdoptedExisting {
		t.Fatalf("observed queue state = %+v", acquisition)
	}
	if len(client.automaticSearches) != 0 || len(client.addRequests) != 0 || len(client.monitorRequests) != 0 {
		t.Fatalf("Existing queue triggered competing work: searches=%v adds=%v monitor=%v",
			client.automaticSearches, client.addRequests, client.monitorRequests)
	}
}

func TestRetryRecreatesMissingMovieOnLockedTargetSnapshot(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{
		{ID: 48, TMDBID: 949, Title: "Heat"},
		{ID: 49, TMDBID: 949, Title: "Heat"},
	}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm Acquisition: %v", err)
	}
	client.getErrors[48] = integrationradarr.ErrNotFound

	recreated, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("retry missing locked movie: %v", err)
	}
	if !recreated.TargetLocked() || recreated.TargetLockedAt == nil || !recreated.TargetLockedAt.Equal(*locked.TargetLockedAt) {
		t.Fatalf("target lock changed during recreation: before=%+v after=%+v", locked, recreated)
	}
	if recreated.TargetInstanceID == nil || *recreated.TargetInstanceID != env.instance.ID ||
		recreated.TargetRootFolderPath != "/media/movies" || recreated.TargetQualityProfileID == nil || *recreated.TargetQualityProfileID != 20 {
		t.Fatalf("target snapshot changed during recreation = %+v", recreated)
	}
	if recreated.RadarrMovieID == nil || *recreated.RadarrMovieID != 49 || recreated.MutationState != "idle" || recreated.Status != "needs_release" {
		t.Fatalf("recreated Acquisition = %+v", recreated)
	}
	if len(client.addRequests) != 2 {
		t.Fatalf("add requests = %d, want initial add and explicit recreation", len(client.addRequests))
	}
	for i, request := range client.addRequests {
		if request.RootFolderPath != "/media/movies" || request.QualityProfileID != 20 || request.Mode != integrationradarr.AcquisitionModeManual {
			t.Fatalf("add request %d changed locked snapshot: %+v", i, request)
		}
	}
}

func TestConcurrentRetryDoesNotDuplicateBlockedRecreation(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{
		{ID: 148, TMDBID: 949, Title: "Heat"},
		{ID: 149, TMDBID: 949, Title: "Heat"},
	}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Acquisition: %v", err)
	}
	client.getErrors[148] = integrationradarr.ErrNotFound
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.addBarrier = barrier
	client.concurrentMu.Unlock()

	type retryResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	firstDone := make(chan retryResult, 1)
	go func() {
		acquisition, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
		firstDone <- retryResult{acquisition: acquisition, err: err}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		close(barrier.release)
		t.Fatal("first retry did not reach AddMovie")
	}
	beforeReview, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		close(barrier.release)
		<-firstDone
		t.Fatalf("read active recreation before abandonment review: %v", err)
	}
	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		close(barrier.release)
		<-firstDone
		t.Fatalf("review active recreation abandonment: %v", err)
	}
	if review.Activity != "unavailable" || review.Acquisition.Revision != beforeReview.Revision ||
		review.Acquisition.NextCheckAt == nil || beforeReview.NextCheckAt == nil ||
		!review.Acquisition.NextCheckAt.Equal(*beforeReview.NextCheckAt) {
		close(barrier.release)
		<-firstDone
		t.Fatalf("abandonment review changed active recreation lease: before=%+v review=%+v", beforeReview, review)
	}

	secondDone := make(chan retryResult, 1)
	go func() {
		acquisition, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
		secondDone <- retryResult{acquisition: acquisition, err: err}
	}()
	var second retryResult
	select {
	case second = <-secondDone:
	case <-time.After(2 * time.Second):
		close(barrier.release)
		<-firstDone
		<-secondDone
		t.Fatal("concurrent retry blocked on a duplicate AddMovie request")
	}
	if second.err != nil {
		close(barrier.release)
		<-firstDone
		t.Fatalf("observe active recreation: %v", second.err)
	}
	if second.acquisition.MutationState != "recreating" || second.acquisition.NextCheckAt == nil ||
		!second.acquisition.NextCheckAt.After(env.service.now()) {
		close(barrier.release)
		<-firstDone
		t.Fatalf("active recreation state = %+v", second.acquisition)
	}
	client.concurrentMu.Lock()
	addRequests := len(client.addRequests)
	client.concurrentMu.Unlock()
	if addRequests != 2 {
		close(barrier.release)
		<-firstDone
		t.Fatalf("AddMovie calls while recreation blocked = %d, want initial add and one recreation", addRequests)
	}

	close(barrier.release)
	select {
	case first := <-firstDone:
		if first.err != nil {
			t.Fatalf("finish first retry: %v", first.err)
		}
		if first.acquisition.RadarrMovieID == nil || *first.acquisition.RadarrMovieID != 149 {
			t.Fatalf("finished recreation = %+v", first.acquisition)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first retry did not finish")
	}
}

func TestExpiredRecreationWorkerCannotPostAfterNewerClaim(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{
		{ID: 158, TMDBID: 949, Title: "Heat"},
		{ID: 159, TMDBID: 949, Title: "Heat"},
	}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Acquisition: %v", err)
	}
	client.getErrors[158] = integrationradarr.ErrNotFound
	client.addErr = integrationradarr.ErrTransient
	interrupted, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("create ambiguous recreation: %v", err)
	}
	if interrupted.MutationState != "recreating" || interrupted.NextCheckAt == nil {
		t.Fatalf("ambiguous recreation = %+v", interrupted)
	}

	var clockMillis atomic.Int64
	clockMillis.Store(interrupted.NextCheckAt.Add(time.Millisecond).UnixMilli())
	env.service.now = func() time.Time { return time.UnixMilli(clockMillis.Load()).UTC() }
	client.addErr = nil
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.findBarrier = barrier
	client.concurrentMu.Unlock()

	type retryResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	staleDone := make(chan retryResult, 1)
	go func() {
		acquisition, retryErr := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
		staleDone <- retryResult{acquisition: acquisition, err: retryErr}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		close(barrier.release)
		t.Fatal("recovered recreation did not reach the movie lookup")
	}
	client.concurrentMu.Lock()
	client.findBarrier = nil
	client.concurrentMu.Unlock()
	firstClaim, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil || firstClaim.NextCheckAt == nil {
		close(barrier.release)
		<-staleDone
		t.Fatalf("read first recovered recreation claim = %+v, err=%v", firstClaim, err)
	}
	clockMillis.Store(firstClaim.NextCheckAt.Add(time.Millisecond).UnixMilli())

	current, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		close(barrier.release)
		<-staleDone
		t.Fatalf("run newer recreation claim: %v", err)
	}
	if current.RadarrMovieID == nil || *current.RadarrMovieID != 159 || current.MutationState != "idle" {
		close(barrier.release)
		<-staleDone
		t.Fatalf("newer recreation = %+v", current)
	}
	close(barrier.release)
	select {
	case stale := <-staleDone:
		if stale.err != nil || stale.acquisition.RadarrMovieID == nil ||
			*stale.acquisition.RadarrMovieID != 159 {
			t.Fatalf("stale recreation completion = %+v, err=%v", stale.acquisition, stale.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale recreation did not finish")
	}
	client.concurrentMu.Lock()
	addRequests := len(client.addRequests)
	client.concurrentMu.Unlock()
	if addRequests != 3 {
		t.Fatalf("AddMovie calls = %d, want initial, ambiguous, and one recovered recreation", addRequests)
	}
}

func TestTimedOutRecreationDefersImmediateRetryAndAdoptsAcceptedMovie(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{
		{ID: 150, TMDBID: 949, Title: "Heat"},
		{ID: 151, TMDBID: 949, Title: "Heat"},
	}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Acquisition: %v", err)
	}
	client.getErrors[150] = integrationradarr.ErrNotFound
	client.addErr = context.DeadlineExceeded
	client.addLandsOnError = true

	ambiguous, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("retry with timed-out recreation: %v", err)
	}
	if ambiguous.MutationState != "recreating" || ambiguous.ActionReason != "connection_failed" ||
		ambiguous.NextCheckAt == nil || !ambiguous.NextCheckAt.After(env.service.now()) {
		t.Fatalf("timed-out recreation state = %+v", ambiguous)
	}
	client.concurrentMu.Lock()
	findCalls := client.findCalls
	addRequests := len(client.addRequests)
	client.concurrentMu.Unlock()

	immediate, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("immediate retry after timeout: %v", err)
	}
	if immediate.Revision != ambiguous.Revision || immediate.MutationState != "recreating" {
		t.Fatalf("immediate retry changed active recreation = %+v", immediate)
	}
	client.concurrentMu.Lock()
	immediateFindCalls := client.findCalls
	immediateAddRequests := len(client.addRequests)
	client.concurrentMu.Unlock()
	if immediateFindCalls != findCalls || immediateAddRequests != addRequests {
		t.Fatalf("immediate retry made remote calls: find=%d/%d add=%d/%d",
			immediateFindCalls, findCalls, immediateAddRequests, addRequests)
	}

	retryAt := ambiguous.NextCheckAt.Add(time.Millisecond)
	env.service.now = func() time.Time { return retryAt }
	client.addErr = nil
	client.addLandsOnError = false
	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("reconcile accepted timed-out recreation: %v", err)
	}
	if recovered.RadarrMovieID == nil || *recovered.RadarrMovieID != 151 ||
		recovered.AdoptedExisting || recovered.MutationState != "idle" {
		t.Fatalf("recovered timed-out recreation = %+v", recovered)
	}
	if len(client.addRequests) != 2 {
		t.Fatalf("AddMovie calls = %d, want initial add and one accepted recreation", len(client.addRequests))
	}
}

func TestRetryRecreatesAutomaticMovieAndStartsSearchInSameRequest(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{
		{ID: 154, TMDBID: 949, Title: "Heat"},
		{ID: 155, TMDBID: 949, Title: "Heat"},
	}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	client.getErrors[154] = integrationradarr.ErrNotFound

	recreated, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("retry missing Automatic movie: %v", err)
	}
	if recreated.RadarrMovieID == nil || *recreated.RadarrMovieID != 155 ||
		recreated.Status != "waiting_for_radarr" || recreated.AutomaticSearchCommandID == nil ||
		*recreated.AutomaticSearchCommandID != 700 {
		t.Fatalf("recreated Automatic Acquisition = %+v", recreated)
	}
	if len(client.automaticSearches) != 2 || client.automaticSearches[0] != 154 || client.automaticSearches[1] != 155 {
		t.Fatalf("Automatic searches = %v, want initial and replacement searches", client.automaticSearches)
	}
}

func TestRetryRecoversAmbiguousAutomaticRecreationAndStartsSearchInSameRequest(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 156, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "automatic", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Automatic Acquisition: %v", err)
	}
	client.getErrors[156] = integrationradarr.ErrNotFound
	client.addErr = integrationradarr.ErrTransient

	interrupted, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("interrupt Automatic recreation: %v", err)
	}
	if interrupted.ActionReason != "connection_failed" || interrupted.MutationState != "recreating" {
		t.Fatalf("interrupted Automatic recreation = %+v", interrupted)
	}
	if interrupted.NextCheckAt == nil {
		t.Fatalf("interrupted Automatic recreation has no retry lease: %+v", interrupted)
	}

	landed := integrationradarr.Movie{ID: 157, TMDBID: 949, Title: "Heat", Monitored: true}
	client.addErr = nil
	client.findMovie = &landed
	client.movies[157] = landed
	retryAt := interrupted.NextCheckAt.Add(time.Millisecond)
	env.service.now = func() time.Time { return retryAt }

	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("recover Automatic recreation: %v", err)
	}
	if recovered.RadarrMovieID == nil || *recovered.RadarrMovieID != 157 || recovered.AdoptedExisting ||
		recovered.Status != "waiting_for_radarr" || recovered.AutomaticSearchCommandID == nil ||
		*recovered.AutomaticSearchCommandID != 700 {
		t.Fatalf("recovered Automatic recreation = %+v", recovered)
	}
	if len(client.automaticSearches) != 2 || client.automaticSearches[0] != 156 || client.automaticSearches[1] != 157 {
		t.Fatalf("Automatic searches = %v, want initial and recovered searches", client.automaticSearches)
	}
}

func TestExistingManualMovieAutoAdoptsAndNeverChangesMonitoring(t *testing.T) {
	client := newFakeRadarrClient()
	remote := integrationradarr.Movie{
		ID: 50, TMDBID: 949, Title: "Heat", Monitored: false,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &remote
	client.movies[50] = remote
	client.releases[50] = []integrationradarr.Release{{
		ID: "existing-release", Title: "Heat.1995.1080p", Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	adopted := env.selectPreset(t)
	if !adopted.AdoptedExisting || !adopted.TargetLocked() || adopted.RadarrMovieID == nil || *adopted.RadarrMovieID != 50 {
		t.Fatalf("recovered Existing Acquisition = %+v", adopted)
	}
	if len(client.addRequests) != 0 || len(client.monitorRequests) != 0 {
		t.Fatalf("Existing adoption mutated Radarr: adds=%v monitor=%v", client.addRequests, client.monitorRequests)
	}

	if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
		t.Fatalf("search releases for adopted movie: %v", err)
	}
	queued, err := env.service.grabRelease(
		env.ctx, env.acquisitionID, "existing-release", false, env.actorID,
	)
	if err != nil {
		t.Fatalf("grab release for adopted movie: %v", err)
	}
	if queued.Status != "queued" || !queued.AdoptedExisting {
		t.Fatalf("adopted movie release state = %+v", queued)
	}
	if len(client.monitorRequests) != 0 || client.movies[50].Monitored {
		t.Fatalf("adopted movie monitoring changed: requests=%v movie=%+v", client.monitorRequests, client.movies[50])
	}
}

func TestRetryAdoptsExternallyReaddedMovieWithoutAnotherAdd(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 51, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("confirm initial Acquisition: %v", err)
	}
	client.getErrors[51] = integrationradarr.ErrNotFound
	external := integrationradarr.Movie{
		ID: 151, TMDBID: 949, Title: "Heat", Monitored: true,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findMovie = &external
	client.movies[151] = external

	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("adopt externally re-added movie: %v", err)
	}
	if recovered.RadarrMovieID == nil || *recovered.RadarrMovieID != 151 || !recovered.AdoptedExisting {
		t.Fatalf("external replacement Acquisition = %+v", recovered)
	}
	if recovered.TargetLockedAt == nil || locked.TargetLockedAt == nil || !recovered.TargetLockedAt.Equal(*locked.TargetLockedAt) {
		t.Fatalf("external replacement changed target lock: before=%+v after=%+v", locked.TargetLockedAt, recovered.TargetLockedAt)
	}
	if recovered.EffectiveConfiguration.RootFolderPath != "/existing/movies" ||
		recovered.EffectiveConfiguration.QualityProfileName != "Legacy profile" ||
		!recovered.EffectiveConfiguration.Monitored ||
		len(recovered.EffectiveConfiguration.Tags) != 1 || recovered.EffectiveConfiguration.Tags[0].Label != "legacy" {
		t.Fatalf("external replacement configuration = %+v", recovered.EffectiveConfiguration)
	}
	if len(client.addRequests) != 1 {
		t.Fatalf("external replacement sent another add: %+v", client.addRequests)
	}
	if len(client.monitorRequests) != 0 {
		t.Fatalf("external replacement monitoring changed: %+v", client.monitorRequests)
	}
}

func TestExternalReplacementLookupFailureKeepsAdoptionSemantics(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 152, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm initial Acquisition: %v", err)
	}
	client.getErrors[152] = integrationradarr.ErrNotFound
	client.findErr = integrationradarr.ErrTransient

	interrupted, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("interrupt replacement lookup: %v", err)
	}
	if interrupted.ActionReason != "connection_failed" {
		t.Fatalf("interrupted replacement lookup = %+v", interrupted)
	}

	external := integrationradarr.Movie{
		ID: 153, TMDBID: 949, Title: "Heat", Monitored: false,
		RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
		MinimumAvailability: integrationradarr.AvailabilityAnnounced,
	}
	client.findErr = nil
	client.findMovie = &external
	client.movies[153] = external

	recovered, err := env.service.retryAcquisition(env.ctx, env.acquisitionID, env.actorID)
	if err != nil {
		t.Fatalf("resume external replacement lookup: %v", err)
	}
	if recovered.RadarrMovieID == nil || *recovered.RadarrMovieID != 153 || !recovered.AdoptedExisting {
		t.Fatalf("external replacement after interrupted lookup = %+v", recovered)
	}
	if len(client.addRequests) != 1 || len(client.monitorRequests) != 0 {
		t.Fatalf("external replacement mutated Radarr: adds=%+v monitor=%+v", client.addRequests, client.monitorRequests)
	}
}

func TestManualGrabRecoveryRestoresMonitoringBeforeLifecycleTransition(t *testing.T) {
	t.Run("active queue", func(t *testing.T) {
		client := newFakeRadarrClient()
		client.addResults = []integrationradarr.Movie{{ID: 52, TMDBID: 949, Title: "Heat"}}
		env := setupRadarrAcquisitionServiceTest(t, "manual", client)
		env.selectPreset(t)
		locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
		if err != nil {
			t.Fatalf("confirm Manual Acquisition: %v", err)
		}
		grabbing, err := env.repo.BeginReleaseAttempt(
			env.ctx, env.acquisitionID, locked.Revision,
			"Heat.1995.1080p", "Bluray-1080p", env.actorID, env.now.Add(2*time.Minute),
		)
		if err != nil {
			t.Fatalf("simulate crash after release claim: %v", err)
		}
		client.queue[52] = []integrationradarr.QueueItem{{
			ID: 7, MovieID: 52, Title: "Heat.1995.1080p", Status: "downloading", TrackedDownloadState: "downloading",
		}}
		var observed repository.RadarrAcquisition
		var observedErr error
		client.onSetMonitored = func(_ int, _ bool) {
			observed, observedErr = env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
		}

		recovered, err := env.service.reconcileOne(env.ctx, grabbing)
		if err != nil {
			t.Fatalf("reconcile claimed release with queue: %v", err)
		}
		if observedErr != nil {
			t.Fatalf("observe state during monitoring: %v", observedErr)
		}
		if observed.MutationState != "grabbing" || observed.Status != "waiting_for_radarr" {
			t.Fatalf("state changed before monitoring: %+v", observed)
		}
		if recovered.Status != "downloading" || recovered.MutationState != "idle" || !client.movies[52].Monitored {
			t.Fatalf("recovered queued Acquisition = %+v, movie=%+v", recovered, client.movies[52])
		}
		if len(client.monitorRequests) != 1 || client.monitorRequests[0] != (monitoredRequest{movieID: 52, monitored: true}) {
			t.Fatalf("monitor requests = %+v", client.monitorRequests)
		}
	})

	t.Run("file available", func(t *testing.T) {
		client := newFakeRadarrClient()
		client.addResults = []integrationradarr.Movie{{ID: 53, TMDBID: 949, Title: "Heat"}}
		env := setupRadarrAcquisitionServiceTest(t, "manual", client)
		env.selectPreset(t)
		locked, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID)
		if err != nil {
			t.Fatalf("confirm Manual Acquisition: %v", err)
		}
		grabbing, err := env.repo.BeginReleaseAttempt(
			env.ctx, env.acquisitionID, locked.Revision,
			"Heat.1995.1080p", "Bluray-1080p", env.actorID, env.now.Add(2*time.Minute),
		)
		if err != nil {
			t.Fatalf("simulate crash after release claim: %v", err)
		}
		movie := client.movies[53]
		movie.HasFile = true
		client.movies[53] = movie
		var observed repository.RadarrAcquisition
		var observedErr error
		client.onSetMonitored = func(_ int, _ bool) {
			observed, observedErr = env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
		}

		recovered, err := env.service.reconcileOne(env.ctx, grabbing)
		if err != nil {
			t.Fatalf("reconcile claimed release with file: %v", err)
		}
		if observedErr != nil {
			t.Fatalf("observe state during monitoring: %v", observedErr)
		}
		if observed.MutationState != "grabbing" || observed.Status != "waiting_for_radarr" {
			t.Fatalf("state changed before monitoring: %+v", observed)
		}
		if recovered.Status != "downloaded" || recovered.MutationState != "idle" || !client.movies[53].Monitored {
			t.Fatalf("recovered downloaded Acquisition = %+v, movie=%+v", recovered, client.movies[53])
		}
		if len(client.monitorRequests) != 1 || client.monitorRequests[0] != (monitoredRequest{movieID: 53, monitored: true}) {
			t.Fatalf("monitor requests = %+v", client.monitorRequests)
		}
	})
}

func TestConcurrentManualGrabClaimsOnlyOneRemoteMutation(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 54, TMDBID: 949, Title: "Heat"}}
	client.releases[54] = []integrationradarr.Release{{
		ID: "one-shot-release", Title: "Heat.1995.1080p", Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
	}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
		t.Fatalf("cache Interactive search results: %v", err)
	}
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.queueBarrier = barrier
	client.concurrentMu.Unlock()

	type grabResult struct {
		acquisition repository.RadarrAcquisition
		err         error
	}
	results := make(chan grabResult, 2)
	for range 2 {
		go func() {
			acquisition, err := env.service.grabRelease(
				env.ctx, env.acquisitionID, "one-shot-release", false, env.actorID,
			)
			results <- grabResult{acquisition: acquisition, err: err}
		}()
	}
	for range 2 {
		select {
		case <-barrier.arrived:
		case <-time.After(3 * time.Second):
			close(barrier.release)
			t.Fatal("concurrent grab did not reach the remote-read barrier")
		}
	}
	close(barrier.release)

	var succeeded, stale int
	for range 2 {
		select {
		case result := <-results:
			switch {
			case result.err == nil:
				succeeded++
				if result.acquisition.Status != "queued" || result.acquisition.MutationState != "idle" {
					t.Fatalf("successful concurrent grab = %+v", result.acquisition)
				}
			case errors.Is(result.err, integration.ErrStaleRevision):
				stale++
			default:
				t.Fatalf("concurrent grab error = %v", result.err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent grab did not finish")
		}
	}
	if succeeded != 1 || stale != 1 {
		t.Fatalf("concurrent grab outcomes: succeeded=%d stale=%d", succeeded, stale)
	}
	if len(client.grabRequests) != 1 || len(client.monitorRequests) != 1 {
		t.Fatalf("concurrent grab remote mutations: grabs=%+v monitor=%+v", client.grabRequests, client.monitorRequests)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition after concurrent grab: %v", err)
	}
	if stored.ManualAttemptCount != 1 || stored.MutationState != "idle" || stored.Status != "queued" {
		t.Fatalf("Acquisition after concurrent grab = %+v", stored)
	}
}

func TestAdminReleaseRemoteObservationsDoNotOverwriteNewerAction(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		observe   string
	}{
		{name: "empty release search", operation: "search", observe: "empty"},
		{name: "search sees file", operation: "search", observe: "file"},
		{name: "search sees identity mismatch", operation: "search", observe: "identity"},
		{name: "grab sees file", operation: "grab", observe: "file"},
		{name: "grab sees identity mismatch", operation: "grab", observe: "identity"},
		{name: "search sees active queue", operation: "search", observe: "queue"},
		{name: "grab sees active queue", operation: "grab", observe: "queue"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeRadarrClient()
			client.addResults = []integrationradarr.Movie{{ID: 60, TMDBID: 949, Title: "Heat"}}
			client.releases[60] = []integrationradarr.Release{{
				ID: "stale-observation-release", Title: "Heat.1995.1080p",
				Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
			}}
			env := setupRadarrAcquisitionServiceTest(t, "manual", client)
			env.selectPreset(t)
			if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
				t.Fatalf("confirm Manual Acquisition: %v", err)
			}
			if test.operation == "grab" {
				if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
					t.Fatalf("cache Interactive search result: %v", err)
				}
			}

			barrier := newFakeCallBarrier()
			client.concurrentMu.Lock()
			switch test.observe {
			case "empty":
				client.releases[60] = nil
				client.releaseBarrier = barrier
			case "file":
				movie := client.movies[60]
				movie.HasFile = true
				client.movies[60] = movie
				client.getMovieBarrier = barrier
			case "identity":
				movie := client.movies[60]
				movie.TMDBID = 12345
				client.movies[60] = movie
				client.getMovieBarrier = barrier
			case "queue":
				client.queue[60] = []integrationradarr.QueueItem{{
					ID: 8, MovieID: 60, Title: "Heat.1995.1080p",
					Status: "downloading", TrackedDownloadState: "downloading",
				}}
				client.queueBarrier = barrier
			}
			client.concurrentMu.Unlock()

			done := make(chan error, 1)
			go func() {
				if test.operation == "grab" {
					_, err := env.service.grabRelease(
						env.ctx, env.acquisitionID, "stale-observation-release", false, env.actorID,
					)
					done <- err
					return
				}
				_, err := env.service.searchReleases(env.ctx, env.acquisitionID)
				done <- err
			}()
			select {
			case <-barrier.arrived:
			case <-time.After(3 * time.Second):
				close(barrier.release)
				t.Fatal("remote observation did not reach the test barrier")
			}
			newer, err := env.repo.TransitionAcquisition(
				env.ctx, env.acquisitionID, repository.RadarrAcquisitionTransition{
					Status: "action_needed", ActionReason: "monitoring_failed",
					FailureSummary: "A newer Admin action must win.", QueueStatus: "none",
					At: env.now.Add(3 * time.Minute),
				},
			)
			if err != nil {
				close(barrier.release)
				t.Fatalf("record newer Admin action: %v", err)
			}
			close(barrier.release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("delayed remote observation did not finish")
			}

			stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
			if err != nil {
				t.Fatalf("read Acquisition after delayed observation: %v", err)
			}
			if stored.Revision != newer.Revision || stored.Status != "action_needed" ||
				stored.ActionReason != "monitoring_failed" || stored.LatestFailureSummary != "A newer Admin action must win." {
				t.Fatalf("delayed %s observation overwrote newer action: newer=%+v stored=%+v", test.observe, newer, stored)
			}
		})
	}
}

func TestAdminReleasePrechecksPersistActiveQueueBeforeConflict(t *testing.T) {
	for _, operation := range []string{"search", "grab"} {
		t.Run(operation, func(t *testing.T) {
			client := newFakeRadarrClient()
			client.addResults = []integrationradarr.Movie{{ID: 61, TMDBID: 949, Title: "Heat"}}
			client.releases[61] = []integrationradarr.Release{{
				ID: "queued-release", Title: "Heat.1995.1080p",
				Quality: integrationradarr.Quality{Name: "Bluray-1080p"}, Approved: true,
			}}
			env := setupRadarrAcquisitionServiceTest(t, "manual", client)
			env.selectPreset(t)
			if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
				t.Fatalf("confirm Manual Acquisition: %v", err)
			}
			if operation == "grab" {
				if _, err := env.service.searchReleases(env.ctx, env.acquisitionID); err != nil {
					t.Fatalf("cache Interactive search result: %v", err)
				}
			}
			client.queue[61] = []integrationradarr.QueueItem{{
				ID: 9, MovieID: 61, Title: "Heat.1995.1080p",
				Status: "downloading", TrackedDownloadState: "downloading",
			}}

			var err error
			if operation == "grab" {
				_, err = env.service.grabRelease(
					env.ctx, env.acquisitionID, "queued-release", false, env.actorID,
				)
			} else {
				_, err = env.service.searchReleases(env.ctx, env.acquisitionID)
			}
			if !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("%s with active queue = %v, want conflict", operation, err)
			}
			stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
			if err != nil {
				t.Fatalf("read Acquisition after active queue: %v", err)
			}
			if stored.Status != "downloading" || stored.QueueStatus != "downloading" ||
				stored.QueueSummary != "Radarr is downloading the selected release." || stored.ActionReason != "" {
				t.Fatalf("%s active queue was not persisted: %+v", operation, stored)
			}
		})
	}
}

func TestSlowerPresetPreviewCannotOverwriteNewerTarget(t *testing.T) {
	for _, test := range []struct {
		name      string
		remote    *integrationradarr.Movie
		remoteErr error
	}{
		{
			name: "older preview succeeds",
			remote: &integrationradarr.Movie{
				ID: 70, TMDBID: 949, Title: "Heat", HasFile: true, Monitored: true,
				RootFolderPath: "/existing/movies", QualityProfileID: 91, TagIDs: []int{92},
				MinimumAvailability: integrationradarr.AvailabilityAnnounced,
			},
		},
		{name: "older preview fails", remoteErr: integrationradarr.ErrTransient},
	} {
		t.Run(test.name, func(t *testing.T) {
			primary := newFakeRadarrClient()
			primary.findMovie = test.remote
			primary.findErr = test.remoteErr
			env := setupRadarrAcquisitionServiceTest(t, "manual", primary)

			secondary := newFakeRadarrClient()
			secondaryURL := "http://radarr-newer-target.test"
			env.clients[secondaryURL] = secondary
			secondaryInstance, err := env.repo.CreateInstance(env.ctx, repository.RadarrInstanceSave{
				Name: "Newer target", BaseURL: secondaryURL, EncryptedAPIKey: []byte("newer-key"),
				State: radarrInstanceConnected, CheckedAt: env.now,
			})
			if err != nil {
				t.Fatalf("create newer Radarr instance: %v", err)
			}
			secondaryPreset, err := env.repo.CreatePreset(env.ctx, repository.RadarrPresetSave{
				Name: "Newer preset", InstanceID: secondaryInstance.ID,
				RootFolderID: 10, RootFolderPath: "/media/movies",
				QualityProfileID: 20, QualityProfileName: "HD-1080p",
				Tags:                []repository.RadarrTagSnapshot{{ID: 30, Label: "movies"}},
				MinimumAvailability: "released", AcquisitionMode: "manual",
				Valid: true, ValidatedAt: env.now,
			})
			if err != nil {
				t.Fatalf("create newer Acquisition preset: %v", err)
			}

			barrier := newFakeCallBarrier()
			primary.concurrentMu.Lock()
			primary.findBarrier = barrier
			primary.concurrentMu.Unlock()
			type previewResult struct {
				acquisition repository.RadarrAcquisition
				err         error
			}
			olderResult := make(chan previewResult, 1)
			go func() {
				acquisition, err := env.service.selectPreset(
					env.ctx, env.acquisitionID, env.preset.ID, env.actorID,
				)
				olderResult <- previewResult{acquisition: acquisition, err: err}
			}()
			select {
			case <-barrier.arrived:
			case <-time.After(3 * time.Second):
				close(barrier.release)
				t.Fatal("older target preview did not reach the remote-read barrier")
			}

			newer, err := env.service.selectPreset(
				env.ctx, env.acquisitionID, secondaryPreset.ID, env.actorID,
			)
			if err != nil {
				close(barrier.release)
				t.Fatalf("select newer Acquisition preset: %v", err)
			}
			if newer.PresetID == nil || *newer.PresetID != secondaryPreset.ID ||
				newer.TargetPreviewExisting || newer.ActionReason != "" {
				close(barrier.release)
				t.Fatalf("newer target preview = %+v", newer)
			}
			close(barrier.release)
			select {
			case result := <-olderResult:
				if result.err != nil {
					t.Fatalf("older target preview completion: %v", result.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("older target preview did not finish")
			}

			stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
			if err != nil {
				t.Fatalf("read final Acquisition target: %v", err)
			}
			if stored.Revision != newer.Revision || stored.PresetID == nil || *stored.PresetID != secondaryPreset.ID ||
				stored.TargetInstanceID == nil || *stored.TargetInstanceID != secondaryInstance.ID ||
				stored.TargetPreviewExisting || stored.EffectiveConfiguration.RootFolderPath != "" ||
				stored.Status != "waiting_for_radarr" || stored.ActionReason != "" || stored.TargetLocked() ||
				stored.RadarrMovieID != nil {
				t.Fatalf("older preview overwrote newer target: newer=%+v stored=%+v", newer, stored)
			}
		})
	}
}

func TestAbandonmentReviewRefreshesCurrentQueueWithoutChangingRadarr(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 80, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	addCount := len(client.addRequests)
	client.queue[80] = []integrationradarr.QueueItem{{
		ID: 10, MovieID: 80, Title: "Heat.1995.1080p",
		Status: "downloading", TrackedDownloadState: "downloading",
	}}

	active, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review active abandonment: %v", err)
	}
	if active.Activity != "active" || active.Acquisition.Status != "downloading" ||
		active.Acquisition.ActionReason != "" || active.Acquisition.QueueStatus != "downloading" {
		t.Fatalf("active abandonment review = %+v", active)
	}
	if len(client.addRequests) != addCount || len(client.grabRequests) != 0 ||
		len(client.monitorRequests) != 0 || len(client.automaticSearches) != 0 {
		t.Fatalf("abandonment review changed Radarr: adds=%+v grabs=%+v monitors=%+v searches=%+v",
			client.addRequests, client.grabRequests, client.monitorRequests, client.automaticSearches)
	}

	client.queue[80] = nil
	inactive, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review inactive abandonment: %v", err)
	}
	if inactive.Activity != "inactive" || inactive.Acquisition.QueueStatus != "none" {
		t.Fatalf("inactive abandonment review = %+v", inactive)
	}
}

func TestAbandonmentQueueTransitionReplacesStaleActionsUnlessTargetIsInvalid(t *testing.T) {
	at := time.Date(2026, time.August, 7, 18, 0, 0, 0, time.UTC)
	tmdbID := 949
	matching := integrationradarr.Movie{ID: 90, TMDBID: 949, Title: "Heat"}
	base := repository.RadarrAcquisition{
		Status: "needs_release", ActionReason: "release_required",
		TMDBID: &tmdbID, TargetAcquisitionMode: "manual",
	}
	for _, test := range []struct {
		name        string
		acquisition repository.RadarrAcquisition
		remote      integrationradarr.Movie
		remoteErr   error
		queue       []integrationradarr.QueueItem
		wantStatus  string
		wantReason  string
		wantQueue   string
	}{
		{
			name: "active queue clears release action", acquisition: base, remote: matching,
			queue:      []integrationradarr.QueueItem{{Status: "downloading", TrackedDownloadState: "downloading"}},
			wantStatus: "downloading", wantQueue: "downloading",
		},
		{
			name: "failed queue replaces no releases",
			acquisition: repository.RadarrAcquisition{
				Status: "action_needed", ActionReason: "no_releases", TMDBID: &tmdbID,
			},
			remote:     matching,
			queue:      []integrationradarr.QueueItem{{Status: "failed", TrackedDownloadState: "failed"}},
			wantStatus: "action_needed", wantReason: "release_failed", wantQueue: "failed",
		},
		{
			name: "identity mismatch remains higher priority", acquisition: base,
			remote:     integrationradarr.Movie{ID: 90, TMDBID: 1234, Title: "Other"},
			queue:      []integrationradarr.QueueItem{{Status: "failed", TrackedDownloadState: "failed"}},
			wantStatus: "action_needed", wantReason: "identity_required", wantQueue: "failed",
		},
		{
			name: "removed movie remains higher priority", acquisition: base,
			remoteErr:  integrationradarr.ErrNotFound,
			queue:      []integrationradarr.QueueItem{{Status: "warning", TrackedDownloadState: "importBlocked"}},
			wantStatus: "action_needed", wantReason: "add_failed", wantQueue: "failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			transition := abandonmentQueueTransition(
				test.acquisition, test.remote, test.remoteErr, test.queue, at,
			)
			if transition.Status != test.wantStatus || transition.ActionReason != test.wantReason ||
				transition.QueueStatus != test.wantQueue {
				t.Fatalf("abandonment queue transition = %+v", transition)
			}
		})
	}
}

func TestAbandonmentRemainsAvailableWhenRadarrCannotBeChecked(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 81, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	client.getErrors[81] = integrationradarr.ErrTransient

	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review unavailable abandonment: %v", err)
	}
	if review.Activity != "unavailable" || review.Acquisition.ActionReason != "connection_failed" {
		t.Fatalf("unavailable abandonment review = %+v", review)
	}
	abandoned, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "The source is unavailable.", review.Activity,
	)
	if err != nil {
		t.Fatalf("abandon after unavailable Radarr check: %v", err)
	}
	if abandoned.Status != "abandoned" || abandoned.AbandonmentReason != "The source is unavailable." ||
		abandoned.LatestFailureSummary != "Radarr could not complete the requested check." {
		t.Fatalf("abandoned Acquisition after unavailable check = %+v", abandoned)
	}
	if len(client.grabRequests) != 0 || len(client.monitorRequests) != 0 || len(client.automaticSearches) != 0 {
		t.Fatalf("abandonment changed Radarr: grabs=%+v monitors=%+v searches=%+v",
			client.grabRequests, client.monitorRequests, client.automaticSearches)
	}
}

func TestAbandonmentRequiresAWarningWhenLiveActivityChangedAfterReview(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 82, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil || review.Activity != "inactive" {
		t.Fatalf("initial abandonment review = %+v, err=%v", review, err)
	}
	client.queue[82] = []integrationradarr.QueueItem{{
		ID: 11, MovieID: 82, Title: "Heat.1995.1080p",
		Status: "downloading", TrackedDownloadState: "downloading",
	}}

	if _, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "No longer needed.", review.Activity,
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("abandon after activity changed = %v, want conflict", err)
	}
	refreshed, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition after changed activity: %v", err)
	}
	if refreshed.Status != "downloading" || refreshed.QueueStatus != "downloading" {
		t.Fatalf("changed activity was not persisted before warning: %+v", refreshed)
	}
	abandoned, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "No longer needed.", "active",
	)
	if err != nil || abandoned.Status != "abandoned" {
		t.Fatalf("abandon after active-work warning = %+v, err=%v", abandoned, err)
	}
}

func TestAbandonmentReviewDoesNotAdoptConcurrentMutationRevision(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 83, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	barrier := newFakeCallBarrier()
	client.concurrentMu.Lock()
	client.queueBarrier = barrier
	client.concurrentMu.Unlock()

	type reviewResult struct {
		review radarrAbandonmentReview
		err    error
	}
	done := make(chan reviewResult, 1)
	go func() {
		review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
		done <- reviewResult{review: review, err: err}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("abandonment review did not reach Queue")
	}

	current, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition before concurrent grab: %v", err)
	}
	grabbing, err := env.repo.BeginReleaseAttempt(
		env.ctx, env.acquisitionID, current.Revision, "Heat.1995.1080p", "Bluray-1080p", env.actorID, env.now,
	)
	if err != nil {
		t.Fatalf("begin concurrent grab: %v", err)
	}
	close(barrier.release)
	result := <-done
	client.concurrentMu.Lock()
	client.queueBarrier = nil
	client.concurrentMu.Unlock()
	if !errors.Is(result.err, integration.ErrStaleRevision) {
		t.Fatalf("stale abandonment review = %+v, err=%v", result.review, result.err)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition after stale review: %v", err)
	}
	if stored.Revision != grabbing.Revision || stored.MutationState != "grabbing" {
		t.Fatalf("stale review changed concurrent mutation: grabbing=%+v stored=%+v", grabbing, stored)
	}

	if _, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "No longer needed.", "inactive",
	); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("abandon without current warning = %v, want conflict", err)
	}
	review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("review concurrent grab: %v", err)
	}
	if review.Activity != "unavailable" || review.Acquisition.MutationState != "grabbing" {
		t.Fatalf("concurrent grab review = %+v", review)
	}
	abandoned, err := env.service.abandonAcquisition(
		env.ctx, env.acquisitionID, env.actorID, "No longer needed.", "unavailable",
	)
	if err != nil || abandoned.Status != "abandoned" {
		t.Fatalf("abandon after current warning = %+v, err=%v", abandoned, err)
	}
}

func TestAbandonmentReviewDoesNotAdoptConcurrentRevisionAsComplete(t *testing.T) {
	client := newFakeRadarrClient()
	client.addResults = []integrationradarr.Movie{{ID: 84, TMDBID: 949, Title: "Heat"}}
	env := setupRadarrAcquisitionServiceTest(t, "manual", client)
	env.selectPreset(t)
	if _, err := env.service.confirmAcquisitionTarget(env.ctx, env.acquisitionID, env.actorID); err != nil {
		t.Fatalf("confirm Manual Acquisition: %v", err)
	}
	remote := client.movies[84]
	remote.HasFile = true
	client.movies[84] = remote
	barrier := newFakeCallBarrier()
	client.getMovieBarrier = barrier

	type reviewResult struct {
		review radarrAbandonmentReview
		err    error
	}
	done := make(chan reviewResult, 1)
	go func() {
		review, err := env.service.reviewAbandonment(env.ctx, env.acquisitionID)
		done <- reviewResult{review: review, err: err}
	}()
	select {
	case <-barrier.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("abandonment review did not reach GetMovie")
	}
	current, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition before concurrent grab: %v", err)
	}
	if _, err := env.repo.BeginReleaseAttempt(
		env.ctx, env.acquisitionID, current.Revision, "Heat.1995.1080p", "Bluray-1080p", env.actorID, env.now,
	); err != nil {
		t.Fatalf("begin concurrent grab: %v", err)
	}
	close(barrier.release)
	result := <-done
	client.concurrentMu.Lock()
	client.getMovieBarrier = nil
	client.concurrentMu.Unlock()
	if !errors.Is(result.err, integration.ErrStaleRevision) {
		t.Fatalf("stale complete review = %+v, err=%v", result.review, result.err)
	}
	stored, err := env.repo.GetVisibleAcquisition(env.ctx, env.acquisitionID)
	if err != nil {
		t.Fatalf("read Acquisition after stale complete review: %v", err)
	}
	if stored.Status == "downloaded" || stored.MutationState != "grabbing" {
		t.Fatalf("stale complete review changed concurrent mutation: %+v", stored)
	}
}
