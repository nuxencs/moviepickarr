package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

//go:embed web/dist
var webFS embed.FS

const (
	MaxPoolSize = 3
	TimeFormat  = time.RFC3339
	ServerPort  = ":3030"
	DbFile      = "moviepickarr.db"
)

var buckets = struct {
	users         string
	watchedMovies string
	nextToWatch   string
	settings      string
}{
	users:         "users",
	watchedMovies: "watched_movies",
	nextToWatch:   "next_to_watch",
	settings:      "settings",
}

type Settings struct {
	PoolLocked bool `json:"poolLocked"`
}

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type Data struct {
	db *bolt.DB
}

type User struct {
	ID          string           `json:"userID"`
	Name        string           `json:"name"`
	CurrentPool map[string]Movie `json:"currentPool"`
	Stash       map[string]Movie `json:"stash"`
	CreatedAt   string           `json:"createdAt"`
}

type Movie struct {
	ID          string `json:"movieID"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	AddedAt     string `json:"addedAt"`
	AddedByID   string `json:"addedByID"`
	AddedByName string `json:"addedByName"`
	WatchedAt   string `json:"watchedAt"`
}

func New() (*Data, error) {
	db, err := bolt.Open(DbFile, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(buckets.users))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(buckets.watchedMovies))
		if err != nil {
			return err
		}
		_, err = tx.CreateBucketIfNotExists([]byte(buckets.nextToWatch))
		if err != nil {
			return err
		}
		b, err := tx.CreateBucketIfNotExists([]byte(buckets.settings))
		if err != nil {
			return err
		}

		// Initialize settings if they don't exist
		if b.Get([]byte("global")) == nil {
			settings := Settings{PoolLocked: false}
			encoded, err := json.Marshal(settings)
			if err != nil {
				return err
			}
			return b.Put([]byte("global"), encoded)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &Data{db: db}, nil
}

func sanitizeInput(input string) string {
	return strings.TrimSpace(input)
}

func (d *Data) getPooledMovies() ([]Movie, error) {
	movies := make([]Movie, 0)

	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))

		return b.ForEach(func(k, v []byte) error {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				return err
			}
			for _, movie := range user.CurrentPool {
				movies = append(movies, movie)
			}
			return nil
		})
	})

	sort.Slice(movies, func(i, j int) bool {
		return movies[i].AddedAt < movies[j].AddedAt
	})

	return movies, err
}

func (d *Data) getUser(userID string) (User, error) {
	var user User
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(userID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}
		return json.Unmarshal(data, &user)
	})

	return user, err
}

func (d *Data) getSettings() (Settings, error) {
	var settings Settings
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.settings))
		data := b.Get([]byte("global"))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "Settings not found")
		}
		return json.Unmarshal(data, &settings)
	})
	return settings, err
}

func (d *Data) handleGetUsers(c *fiber.Ctx) error {
	users := make([]User, 0)

	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		return b.ForEach(func(k, v []byte) error {
			var user User
			if err := json.Unmarshal(v, &user); err != nil {
				return err
			}
			users = append(users, user)
			return nil
		})
	})
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].CreatedAt < users[j].CreatedAt
	})

	/*return c.Status(fiber.StatusOK).JSON(Response{
		Success: true,
		Data:    users,
	})*/
	return c.Status(fiber.StatusOK).JSON(users)
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

	user := User{
		ID:          uuid.New().String(),
		Name:        sanitizeInput(body.Name),
		CurrentPool: make(map[string]Movie),
		Stash:       make(map[string]Movie),
		CreatedAt:   time.Now().Format(TimeFormat),
	}

	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		encoded, err := json.Marshal(user)
		if err != nil {
			return err
		}
		return b.Put([]byte(user.ID), encoded)
	})
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.Status(fiber.StatusCreated).JSON(Response{
		Success: true,
		Data:    user,
	})*/
	return c.Status(fiber.StatusCreated).JSON(user)
}

func (d *Data) handleDeleteUser(c *fiber.Ctx) error {
	type request struct {
		UserID string `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		if b.Get([]byte(body.UserID)) == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}
		return b.Delete([]byte(body.UserID))
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "User not found" {
			status = fiber.StatusNotFound
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

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

	var settings Settings
	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.settings))
		settings.PoolLocked = body.Lock
		encoded, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		return b.Put([]byte("global"), encoded)
	})
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	return c.Status(fiber.StatusOK).JSON(settings)
}

func (d *Data) handleGetPoolLock(c *fiber.Ctx) error {
	settings, err := d.getSettings()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	return c.Status(fiber.StatusOK).JSON(settings.PoolLocked)
}

func (d *Data) handleAddMovie(c *fiber.Ctx) error {
	type request struct {
		UserID string `json:"userID"`
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

	if body.UserID == "" || body.Title == "" || body.Link == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID, Title and link are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	user, err := d.getUser(body.UserID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	movie := Movie{
		ID:          uuid.New().String(),
		Title:       sanitizeInput(body.Title),
		Link:        sanitizeInput(body.Link),
		AddedAt:     time.Now().Format(TimeFormat),
		AddedByID:   body.UserID,
		AddedByName: user.Name,
	}

	settings, err := d.getSettings()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	err = d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(body.UserID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		// If pool is locked or pool is full, add to stash
		if settings.PoolLocked || len(user.CurrentPool) >= MaxPoolSize {
			user.Stash[movie.ID] = movie
		} else {
			user.CurrentPool[movie.ID] = movie
		}

		encoded, err := json.Marshal(user)
		if err != nil {
			return err
		}

		return b.Put([]byte(user.ID), encoded)
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "User not found" {
			status = fiber.StatusNotFound
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.Status(fiber.StatusCreated).JSON(Response{
		Success: true,
		Data:    movie,
	})*/
	return c.Status(fiber.StatusCreated).JSON(movie)
}

func (d *Data) handleGetPool(c *fiber.Ctx) error {
	type request struct {
		UserID string `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	var currentPool map[string]Movie
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(body.UserID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		currentPool = user.CurrentPool
		return nil
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "User not found" {
			status = fiber.StatusNotFound
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    currentPool,
	})*/
	return c.Status(fiber.StatusOK).JSON(currentPool)
}

func (d *Data) handleDeleteMovie(c *fiber.Ctx) error {
	type request struct {
		UserID  string `json:"userID"`
		MovieID string `json:"movieID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == "" || body.MovieID == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID and MovieID are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(body.UserID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		if len(user.CurrentPool) == 0 && len(user.Stash) == 0 {
			return fiber.NewError(fiber.StatusBadRequest, "Pool and Stash are empty")
		}

		delete(user.CurrentPool, body.MovieID)
		delete(user.Stash, body.MovieID)

		encoded, err := json.Marshal(user)
		if err != nil {
			return err
		}

		return b.Put([]byte(user.ID), encoded)
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "User not found" {
			status = fiber.StatusNotFound
		} else if err.Error() == "Pool and Stash are empty" {
			status = fiber.StatusBadRequest
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    "Movie deleted",
	})*/
	return c.Status(fiber.StatusNoContent).JSON(nil)
}

func (d *Data) handleGetStash(c *fiber.Ctx) error {
	type request struct {
		UserID string `json:"userID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID is required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	var stash map[string]Movie
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(body.UserID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		stash = user.Stash
		return nil
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "User not found" {
			status = fiber.StatusNotFound
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    stash,
	})*/
	return c.Status(fiber.StatusOK).JSON(stash)
}

func (d *Data) handleMove(c *fiber.Ctx) error {
	settings, err := d.getSettings()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	if settings.PoolLocked {
		return c.Status(fiber.StatusForbidden).JSON(nil)
	}

	type request struct {
		UserID  string `json:"userID"`
		MovieID string `json:"movieID"`
	}
	var body request
	if err := c.BodyParser(&body); err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "Invalid input",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	if body.UserID == "" || body.MovieID == "" {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   "UserID and MovieID are required",
		})*/
		return c.Status(fiber.StatusBadRequest).JSON(nil)
	}

	var updatedUser User
	err = d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(body.UserID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		movie, isStashed := user.Stash[body.MovieID]
		if !isStashed {
			movie, ok := user.CurrentPool[body.MovieID]
			if !ok {
				return fiber.NewError(fiber.StatusNotFound, "Movie not found")
			}
			delete(user.CurrentPool, body.MovieID)
			user.Stash[body.MovieID] = movie
		} else {
			if len(user.CurrentPool) >= MaxPoolSize {
				return fiber.NewError(fiber.StatusBadRequest, "Pool is already full")
			}
			delete(user.Stash, body.MovieID)
			user.CurrentPool[body.MovieID] = movie
		}

		encoded, err := json.Marshal(user)
		if err != nil {
			return err
		}

		updatedUser = user
		return b.Put([]byte(user.ID), encoded)
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		switch err.Error() {
		case "User not found", "Movie not found":
			status = fiber.StatusNotFound
		case "Pool is already full":
			status = fiber.StatusBadRequest
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    updatedUser,
	})*/
	return c.Status(fiber.StatusOK).JSON(updatedUser)
}

func (d *Data) handleGetPooledMovies(c *fiber.Ctx) error {
	movies, err := d.getPooledMovies()
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
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.nextToWatch))
		data := b.Get([]byte("current"))
		if data != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "There is already a movie in next to watch")
		}
		return nil
	})
	if err != nil {
		/*return c.Status(fiber.StatusBadRequest).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	movies, err := d.getPooledMovies()
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	if len(movies) == 0 {
		/*return c.JSON(Response{
			Success: false,
			Error:   "There are no movies to watch",
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	randomIndex := rand.Intn(len(movies))
	selectedMovie := movies[randomIndex]

	err = d.db.Update(func(tx *bolt.Tx) error {
		// Remove from user's pool
		b := tx.Bucket([]byte(buckets.users))
		data := b.Get([]byte(selectedMovie.AddedByID))
		if data == nil {
			return fiber.NewError(fiber.StatusNotFound, "User not found")
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return err
		}

		delete(user.CurrentPool, selectedMovie.ID)

		encoded, err := json.Marshal(user)
		if err != nil {
			return err
		}

		err = b.Put([]byte(user.ID), encoded)
		if err != nil {
			return err
		}

		// Set as current movie
		b = tx.Bucket([]byte(buckets.nextToWatch))
		encoded, err = json.Marshal(selectedMovie)
		if err != nil {
			return err
		}

		return b.Put([]byte("current"), encoded)
	})
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    "Found random movie to watch",
	})*/
	return c.Status(fiber.StatusOK).JSON(selectedMovie)
}

func (d *Data) handleGetCurrentMovie(c *fiber.Ctx) error {
	var movie *Movie
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.nextToWatch))
		data := b.Get([]byte("current"))
		if data == nil {
			movie = nil
			return nil
		}
		return json.Unmarshal(data, &movie)
	})
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    &movie,
	})*/
	return c.Status(fiber.StatusOK).JSON(movie)
}

func (d *Data) handleWatchMovie(c *fiber.Ctx) error {
	var watchedMovie Movie
	err := d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.nextToWatch))
		data := b.Get([]byte("current"))
		if data == nil {
			return fiber.NewError(fiber.StatusBadRequest, "There is no movie to watch")
		}

		if err := json.Unmarshal(data, &watchedMovie); err != nil {
			return err
		}

		watchedMovie.WatchedAt = time.Now().Format(TimeFormat)

		// Add to watched movies
		b = tx.Bucket([]byte(buckets.watchedMovies))
		encoded, err := json.Marshal(watchedMovie)
		if err != nil {
			return err
		}

		err = b.Put([]byte(watchedMovie.ID), encoded)
		if err != nil {
			return err
		}

		// Clear current movie
		b = tx.Bucket([]byte(buckets.nextToWatch))
		return b.Delete([]byte("current"))
	})
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "There is no movie to watch" {
			status = fiber.StatusBadRequest
		}
		/*return c.Status(status).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(status).JSON(nil)
	}

	/*return c.JSON(Response{
		Success: true,
		Data:    "Movie watched",
	})*/
	return c.Status(fiber.StatusOK).JSON(watchedMovie)
}

func (d *Data) handleGetWatchedMovies(c *fiber.Ctx) error {
	movies := make([]Movie, 0)
	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(buckets.watchedMovies))
		return b.ForEach(func(k, v []byte) error {
			var movie Movie
			if err := json.Unmarshal(v, &movie); err != nil {
				return err
			}
			movies = append(movies, movie)
			return nil
		})
	})
	if err != nil {
		/*return c.Status(fiber.StatusInternalServerError).JSON(Response{
			Success: false,
			Error:   err.Error(),
		})*/
		return c.Status(fiber.StatusInternalServerError).JSON(nil)
	}

	sort.Slice(movies, func(i, j int) bool {
		return movies[i].AddedAt > movies[j].AddedAt
	})

	/*return c.JSON(Response{
		Success: true,
		Data:    movies,
	})*/
	return c.Status(fiber.StatusOK).JSON(movies)
}

func main() {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})
	app.Use(logger.New(logger.Config{
		TimeFormat: "2006-01-02 15:04:05",
		TimeZone:   "Local",
	}))
	app.Use(cors.New())

	// Serve the web build
	webRoot, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatal(err)
	}

	app.Use("/", filesystem.New(filesystem.Config{
		Root: http.FS(webRoot),
	}))

	data, err := New()
	if err != nil {
		log.Fatal(err)
	}
	defer data.db.Close()

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		log.Println("Gracefully shutting down...")
		_ = app.Shutdown()
	}()

	api := app.Group("/api")
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

	log.Fatal(app.Listen(ServerPort))
}
