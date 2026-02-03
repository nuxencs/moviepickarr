package main

import (
	"bufio"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/nextpicker"
	"moviepickarr/internal/repository"
	"moviepickarr/internal/settings"
	"moviepickarr/internal/user"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/joho/godotenv"
	_ "modernc.org/sqlite"
)

//go:embed web/dist
var webFS embed.FS

const (
	MaxPoolSize = 3
	TimeFormat  = time.RFC3339
	ServerPort  = ":3030"
	DbFile      = "moviepickarr.db"
)

var imdbIDRegex = regexp.MustCompile(`tt\d{7,8}`)

type Settings struct {
	PoolLocked bool `json:"poolLocked"`
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type Data struct {
	broker            *EventBroker
	userService       user.Service
	movieService      movie.Service
	nextPickerService nextpicker.Service
	settingsService   settings.Service
}

func (d *Data) Close() {
	if d == nil || d.broker == nil {
		return
	}
	d.broker.Close()
}

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

type EventBroker struct {
	clients map[chan Event]bool
	mu      sync.RWMutex
}

func NewEventBroker() *EventBroker {
	return &EventBroker{
		clients: make(map[chan Event]bool),
	}
}

func (b *EventBroker) Subscribe() chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	client := make(chan Event, 10)
	b.clients[client] = true
	return client
}

func (b *EventBroker) Unsubscribe(client chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.clients[client]; ok {
		delete(b.clients, client)
		close(client)
	}
}

func (b *EventBroker) Broadcast(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for client := range b.clients {
		select {
		case client <- event:
		default:
			// Client channel is full, skip
		}
	}
}

func (b *EventBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for client := range b.clients {
		close(client)
		delete(b.clients, client)
	}
}

type User struct {
	ID          int              `json:"userID"`
	Name        string           `json:"name"`
	CurrentPool map[string]Movie `json:"currentPool"`
	Stash       map[string]Movie `json:"stash"`
	CreatedAt   string           `json:"createdAt"`
}

type Movie struct {
	ID          int    `json:"movieID"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	AddedAt     string `json:"addedAt"`
	AddedByID   int    `json:"addedByID"`
	AddedByName string `json:"addedByName"`
	WatchedAt   string `json:"watchedAt"`
}

type TMDBMovie struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	PosterPath  *string `json:"poster_path"`
	ReleaseDate string  `json:"release_date"`
	Overview    string  `json:"overview"`
}

type TMDBSearchResponse struct {
	Results []TMDBMovie `json:"results"`
}

type TMDBExternalIDsResponse struct {
	IMDbID string `json:"imdb_id"`
}

func NewData(dbConn *sql.DB) *Data {
	userRepo := repository.NewSqliteUserRepository(dbConn)
	movieRepo := repository.NewSqliteMoviesRepository(dbConn)
	nextPickerRepo := repository.NewSqliteNextPickerRepository(dbConn)
	settingsRepo := repository.NewSqliteSettingsRepository(dbConn)

	return &Data{
		broker:            NewEventBroker(),
		userService:       user.NewService(userRepo, nextPickerRepo),
		movieService:      movie.NewService(movieRepo, settingsRepo),
		nextPickerService: nextpicker.NewService(nextPickerRepo, userRepo),
		settingsService:   settings.NewService(settingsRepo),
	}
}

func sanitizeInput(input string) string {
	return strings.TrimSpace(input)
}

func sanitizeLink(link string) string {
	link = strings.TrimSpace(link)

	if strings.Contains(link, "imdb.com") {
		match := imdbIDRegex.FindString(link)

		if match != "" {
			return "https://www.imdb.com/title/" + match + "/"
		}
	}

	return link
}

func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(TimeFormat)
}

func toAPIMovie(movie *domain.Movie) Movie {
	return Movie{
		ID:          movie.ID,
		Title:       movie.Title,
		Link:        movie.Link,
		AddedAt:     formatTime(movie.AddedAt),
		AddedByID:   movie.AddedByID,
		AddedByName: movie.AddedByName,
		WatchedAt:   formatTime(movie.WatchedAt),
	}
}

func toAPIMovies(movies []*domain.Movie) []Movie {
	result := make([]Movie, 0, len(movies))
	for _, movie := range movies {
		result = append(result, toAPIMovie(movie))
	}
	return result
}

func moviesToMap(movies []*domain.Movie) map[string]Movie {
	result := make(map[string]Movie, len(movies))
	for _, movie := range movies {
		result[strconv.Itoa(movie.ID)] = toAPIMovie(movie)
	}
	return result
}

func toAPIUser(user *domain.User, poolMovies, stashMovies []*domain.Movie) User {
	currentPool := moviesToMap(poolMovies)
	stash := moviesToMap(stashMovies)

	return User{
		ID:          user.ID,
		Name:        user.Name,
		CurrentPool: currentPool,
		Stash:       stash,
		CreatedAt:   formatTime(user.CreatedAt),
	}
}

func (d *Data) getPooledMovies(ctx context.Context) ([]Movie, error) {
	movies, err := d.movieService.Pooled(ctx)
	if err != nil {
		return nil, err
	}
	return toAPIMovies(movies), nil
}

func (d *Data) initNextPicker(ctx context.Context) error {
	users, err := d.userService.List(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}

	return d.nextPickerService.Set(ctx, users[0].ID)
}

func (d *Data) advanceNextPicker(ctx context.Context) error {
	users, err := d.userService.List(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 || len(users) == 1 {
		return nil
	}

	pooled, err := d.movieService.Pooled(ctx)
	if err != nil {
		return err
	}
	if len(pooled) == 0 {
		return nil
	}

	current, err := d.nextPickerService.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := d.initNextPicker(ctx); err != nil {
				return err
			}
			current, err = d.nextPickerService.Get(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
		}
		if err != nil {
			return err
		}
	}

	currentIndex := -1
	for i, user := range users {
		if current != nil && user.ID == current.ID {
			currentIndex = i
			break
		}
	}

	nextIndex := 0
	if currentIndex >= 0 {
		nextIndex = (currentIndex + 1) % len(users)
	}

	if err := d.nextPickerService.Set(ctx, users[nextIndex].ID); err != nil {
		return err
	}

	d.broker.Broadcast(Event{
		Type: "settings:next-picker-changed",
		Data: map[string]any{
			"id":   users[nextIndex].ID,
			"name": users[nextIndex].Name,
		},
	})

	return nil
}

func (d *Data) handleGetUsers(c *fiber.Ctx) error {
	ctx := c.UserContext()

	users, err := d.userService.List(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	movies, err := d.movieService.List(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	poolByUser := make(map[int]map[string]Movie)
	stashByUser := make(map[int]map[string]Movie)

	for _, movie := range movies {
		if movie.Status != "pool" && movie.Status != "stash" {
			continue
		}

		apiMovie := toAPIMovie(movie)
		key := strconv.Itoa(movie.ID)

		if movie.Status == "pool" {
			if poolByUser[movie.AddedByID] == nil {
				poolByUser[movie.AddedByID] = map[string]Movie{}
			}
			poolByUser[movie.AddedByID][key] = apiMovie
			continue
		}

		if stashByUser[movie.AddedByID] == nil {
			stashByUser[movie.AddedByID] = map[string]Movie{}
		}
		stashByUser[movie.AddedByID][key] = apiMovie
	}

	response := make([]User, 0, len(users))
	for _, user := range users {
		currentPool := poolByUser[user.ID]
		if currentPool == nil {
			currentPool = map[string]Movie{}
		}
		stash := stashByUser[user.ID]
		if stash == nil {
			stash = map[string]Movie{}
		}

		response = append(response, User{
			ID:          user.ID,
			Name:        user.Name,
			CurrentPool: currentPool,
			Stash:       stash,
			CreatedAt:   formatTime(user.CreatedAt),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func (d *Data) handleCreateUser(c *fiber.Ctx) error {
	type request struct {
		Name string `json:"name"`
	}
	var body request

	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.Name == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Name is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	createdUser, err := d.userService.Create(ctx, sanitizeInput(body.Name))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	user := User{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		CurrentPool: map[string]Movie{},
		Stash:       map[string]Movie{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	d.broker.Broadcast(Event{
		Type: "user:created",
		Data: user,
	})

	/*return c.Status(fiber.StatusCreated).JSON(Response{
		Success: true,
		Data:    user,
	})*/
	return c.Status(fiber.StatusCreated).JSON(user)
}

func (d *Data) handleDeleteUser(c *fiber.Ctx) error {
	type request struct {
		UserID int `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	if err := d.userService.Delete(ctx, body.UserID); err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	d.broker.Broadcast(Event{
		Type: "user:deleted",
		Data: fiber.Map{"userID": body.UserID},
	})

	/*return c.JSON(Response{
		Success: true,
		Data:    "User deleted",
	})*/
	return c.Status(fiber.StatusNoContent).JSON(nil)
}

func (d *Data) handleTogglePoolLock(c *fiber.Ctx) error {
	type request struct {
		Lock bool `json:"lock"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	if err := d.settingsService.SetPoolLock(ctx, body.Lock); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	settings := Settings{PoolLocked: body.Lock}

	d.broker.Broadcast(Event{
		Type: "settings:pool-lock-changed",
		Data: settings,
	})

	return c.Status(fiber.StatusOK).JSON(settings)
}

func (d *Data) handleGetPoolLock(c *fiber.Ctx) error {
	ctx := c.UserContext()
	poolLocked, err := d.settingsService.GetPoolLock(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	return c.Status(fiber.StatusOK).JSON(poolLocked)
}

func (d *Data) handleGetNextPicker(c *fiber.Ctx) error {
	ctx := c.UserContext()

	nextPicker, err := d.nextPickerService.Get(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if err := d.initNextPicker(ctx); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(nil)
			}
			nextPicker, err = d.nextPickerService.Get(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				return c.Status(fiber.StatusOK).JSON(fiber.Map{
					"id":   0,
					"name": "",
				})
			}
		}
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(nil)
		}
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"id":   nextPicker.ID,
		"name": nextPicker.Name,
	})
}

func (d *Data) handleAddMovie(c *fiber.Ctx) error {
	type request struct {
		UserID int    `json:"userID"`
		Title  string `json:"title"`
		Link   string `json:"link"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 || body.Title == "" || body.Link == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID, Title and link are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	if _, err := d.userService.Get(ctx, body.UserID); err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	title := sanitizeInput(body.Title)
	link := sanitizeLink(body.Link)

	movieRecord, err := d.movieService.AddToPool(ctx, title, link, body.UserID)
	if err != nil {
		if err.Error() == "pool limit reached" || err.Error() == "pool is locked" {
			movieRecord, err = d.movieService.AddToStash(ctx, title, link, body.UserID)
		}
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	movie := toAPIMovie(movieRecord)

	d.broker.Broadcast(Event{
		Type: "movie:added",
		Data: movie,
	})

	/*return c.Status(fiber.StatusCreated).JSON(Response{
		Success: true,
		Data:    movie,
	})*/
	return c.Status(fiber.StatusCreated).JSON(movie)
}

func (d *Data) handleGetPool(c *fiber.Ctx) error {
	type request struct {
		UserID int `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	if _, err := d.userService.Get(ctx, body.UserID); err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}
	movies, err := d.movieService.PooledByUserID(ctx, body.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    currentPool,
	})*/
	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}

func (d *Data) handleDeleteMovie(c *fiber.Ctx) error {
	type request struct {
		UserID  int `json:"userID"`
		MovieID int `json:"movieID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 || body.MovieID == 0 {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID and MovieID are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	movieRecord, err := d.movieService.Get(ctx, body.MovieID)
	if err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	if movieRecord.AddedByID != body.UserID {
		return c.Status(fiber.StatusNotFound).JSON(nil)
	}

	if movieRecord.Status != "pool" && movieRecord.Status != "stash" {
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if err := d.movieService.Delete(ctx, body.MovieID); err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	d.broker.Broadcast(Event{
		Type: "movie:deleted",
		Data: fiber.Map{
			"userID":  body.UserID,
			"movieID": body.MovieID,
		},
	})

	/*return c.JSON(Response{
		Success: true,
		Data:    "Movie deleted",
	})*/
	return c.Status(fiber.StatusNoContent).JSON(nil)
}

func (d *Data) handleGetStash(c *fiber.Ctx) error {
	type request struct {
		UserID int `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()
	if _, err := d.userService.Get(ctx, body.UserID); err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}
	movies, err := d.movieService.StashedByUserID(ctx, body.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    stash,
	})*/
	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}

func (d *Data) handleMove(c *fiber.Ctx) error {
	type request struct {
		UserID  int `json:"userID"`
		MovieID int `json:"movieID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == 0 || body.MovieID == 0 {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID and MovieID are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	ctx := c.UserContext()

	poolLocked, err := d.settingsService.GetPoolLock(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}
	if poolLocked {
		return c.Status(fiber.StatusForbidden).JSON(nil)
	}

	userRecord, err := d.userService.Get(ctx, body.UserID)
	if err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	movieRecord, err := d.movieService.Get(ctx, body.MovieID)
	if err != nil {
		status := fiber.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusNotFound
		}
		return c.Status(status).JSON(nil)
	}

	if movieRecord.AddedByID != body.UserID {
		return c.Status(fiber.StatusNotFound).JSON(nil)
	}

	switch movieRecord.Status {
	case "stash":
		pooled, err := d.movieService.PooledByUserID(ctx, body.UserID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(nil)
		}
		if len(pooled) >= MaxPoolSize {
			return c.Status(fiber.StatusBadRequest).JSON(nil)
		}
		if err := d.movieService.MoveToPool(ctx, body.MovieID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(nil)
		}
	case "pool":
		if err := d.movieService.MoveToStash(ctx, body.MovieID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(nil)
		}
	default:
		return c.Status(fiber.StatusNotFound).JSON(nil)
	}

	updatedPool, err := d.movieService.PooledByUserID(ctx, body.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}
	updatedStash, err := d.movieService.StashedByUserID(ctx, body.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	updatedUser := toAPIUser(userRecord, updatedPool, updatedStash)

	d.broker.Broadcast(Event{
		Type: "movie:moved",
		Data: updatedUser,
	})

	/*return c.JSON(Response{
		Success: true,
		Data:    updatedUser,
	})*/
	return c.Status(fiber.StatusOK).JSON(updatedUser)
}

func (d *Data) handleGetPooledMovies(c *fiber.Ctx) error {
	ctx := c.UserContext()
	movies, err := d.getPooledMovies(ctx)
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    movies,
	})*/
	return c.Status(fiber.StatusOK).JSON(movies)
}

func (d *Data) handleGetRandomMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	selectedMovie, err := d.movieService.PickRandom(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	if err := d.advanceNextPicker(ctx); err != nil {
		log.Printf("Failed to advance next picker: %v", err)
	}

	movie := toAPIMovie(selectedMovie)

	d.broker.Broadcast(Event{
		Type: "movie:picked",
		Data: movie,
	})

	return c.Status(fiber.StatusOK).JSON(movie)
}

func (d *Data) handleGetCurrentMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	movieRecord, err := d.movieService.Current(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusOK).JSON(nil)
		}
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovie(movieRecord))
}

func (d *Data) handleWatchMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	watched, err := d.movieService.MarkCurrentAsWatched(ctx)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "no current movie" || errors.Is(err, sql.ErrNoRows) {
			status = fiber.StatusBadRequest
		}
		return c.Status(status).JSON(nil)
	}

	watchedMovie := toAPIMovie(watched)

	d.broker.Broadcast(Event{
		Type: "movie:watched",
		Data: watchedMovie,
	})

	return c.Status(fiber.StatusOK).JSON(watchedMovie)
}

func (d *Data) handleGetWatchedMovies(c *fiber.Ctx) error {
	ctx := c.UserContext()
	movies, err := d.movieService.Watched(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMovies(movies))
}

func (d *Data) handleTMDBSearch(c *fiber.Ctx) error {
	query := c.Query("query")
	if query == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "query parameter is required",
		})
	}

	tmdbAPIKey := os.Getenv("TMDB_API_KEY")
	if tmdbAPIKey == "" {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "TMDB API key not configured",
		})
	}

	tmdbURL := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?query=%s",
		url.QueryEscape(query))

	req, err := http.NewRequest(http.MethodGet, tmdbURL, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create request",
		})
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tmdbAPIKey))
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to search TMDB",
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "TMDB API request failed",
		})
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to read TMDB response",
		})
	}

	var searchResponse TMDBSearchResponse
	if err := json.Unmarshal(body, &searchResponse); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to parse TMDB response",
		})
	}

	return c.Status(fiber.StatusOK).JSON(searchResponse.Results)
}

func (d *Data) handleSSE(c *fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		eventChannel := d.broker.Subscribe()
		defer d.broker.Unsubscribe(eventChannel)

		_, err := fmt.Fprintf(w, "event: connected\ndata: {\"type\":\"connected\"}\n\n")
		if err != nil {
			log.Printf("Error writing to client: %v", err)
			return
		}
		if err := w.Flush(); err != nil {
			log.Printf("Error flushing client: %v", err)
			return
		}

		for event := range eventChannel {
			eventData, err := json.Marshal(event)
			if err != nil {
				log.Printf("Error marshalling event: %v", err)
				continue
			}

			_, err = fmt.Fprintf(w, "event: message\ndata: %s\n\n", eventData)
			if err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		}
	})

	return nil
}

func (d *Data) handleTMDBExternalIDs(c *fiber.Ctx) error {
	movieID := c.Query("movieId")
	if movieID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "movieId parameter is required",
		})
	}

	tmdbAPIKey := os.Getenv("TMDB_API_KEY")
	if tmdbAPIKey == "" {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "TMDB API key not configured",
		})
	}

	tmdbURL := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s/external_ids", movieID)

	req, err := http.NewRequest(http.MethodGet, tmdbURL, nil)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create request",
		})
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tmdbAPIKey))
	req.Header.Set("Accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch TMDB external IDs",
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "TMDB API request failed",
		})
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to read TMDB response",
		})
	}

	var externalIDs TMDBExternalIDsResponse
	if err := json.Unmarshal(body, &externalIDs); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to parse TMDB response",
		})
	}

	link := fmt.Sprintf("https://www.themoviedb.org/movie/%s", movieID)
	if externalIDs.IMDbID != "" {
		link = fmt.Sprintf("https://www.imdb.com/title/%s/", externalIDs.IMDbID)
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"link": link,
	})
}

func main() {
	ctx := context.Background()

	_ = godotenv.Load()

	if _, err := db.MigrateBoltToSQLite(ctx, DbFile, DbFile); err != nil {
		log.Fatal(err)
	}

	dbConn, err := db.OpenSQLite(DbFile)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.RunMigrations(ctx, dbConn); err != nil {
		log.Fatal(err)
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logger.New(logger.Config{
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
	}))
	app.Use(cors.New())

	data := NewData(dbConn)

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)

	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			log.Println("Gracefully shutting down...")
			ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := app.ShutdownWithContext(ctxTimeout); err != nil {
				log.Printf("shutdown error: %v", err)
			}
			data.Close()
			if err := dbConn.Close(); err != nil {
				log.Printf("db close error: %v", err)
			}
		})
	}
	go func() {
		<-c
		shutdown()
	}()

	api := app.Group("/api")

	api.Get("/events", data.handleSSE)

	usersAPI := api.Group("/users")
	moviesAPI := api.Group("/movies")
	settingsAPI := api.Group("/settings")

	usersAPI.Get("/list", data.handleGetUsers)
	usersAPI.Post("/create", data.handleCreateUser)
	usersAPI.Delete("/delete", data.handleDeleteUser)
	usersAPI.Post("/movie/add", data.handleAddMovie)
	usersAPI.Delete("/movie/delete", data.handleDeleteMovie)
	usersAPI.Post("/movie/move", data.handleMove)
	usersAPI.Get("/pool", data.handleGetPool)
	usersAPI.Get("/stash", data.handleGetStash)

	moviesAPI.Get("/listpool", data.handleGetPooledMovies)
	moviesAPI.Post("/random", data.handleGetRandomMovie)
	moviesAPI.Get("/current", data.handleGetCurrentMovie)
	moviesAPI.Get("/listwatched", data.handleGetWatchedMovies)
	moviesAPI.Post("/markwatched", data.handleWatchMovie)

	settingsAPI.Get("/getlock", data.handleGetPoolLock)
	settingsAPI.Post("/togglelock", data.handleTogglePoolLock)
	settingsAPI.Get("/nextpicker", data.handleGetNextPicker)

	tmdbAPI := api.Group("/tmdb")
	tmdbAPI.Get("/search", data.handleTMDBSearch)
	tmdbAPI.Get("/external-ids", data.handleTMDBExternalIDs)

	webRoot, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}

	app.Use("/", filesystem.New(filesystem.Config{
		Root: http.FS(webRoot),
	}))

	if err := app.Listen(ServerPort); err != nil {
		log.Printf("listen error: %v", err)
	}

	shutdown()
}
