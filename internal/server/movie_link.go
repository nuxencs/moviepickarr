package server

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"moviepickarr/internal/domain"
)

var (
	canonicalIMDbIDRegex      = regexp.MustCompile(`^tt\d{7,8}$`)
	imdbMoviePathRegex        = regexp.MustCompile(`(?i)^/title/(tt\d{7,8})/?$`)
	tmdbMoviePathRegex        = regexp.MustCompile(`^/movie/([1-9]\d*)(?:-[^/\\]+)?/?$`)
	encodedPathSeparatorRegex = regexp.MustCompile(`(?i)%2f|%5c`)
)

const maxSafeInteger = int64(1<<53 - 1)

func parseMovieLink(raw string) (domain.MovieIdentityTarget, error) {
	invalid := func() (domain.MovieIdentityTarget, error) {
		return domain.MovieIdentityTarget{}, fmt.Errorf(
			"%w: link must be an IMDb or TMDB movie URL",
			domain.ErrInvalidInput,
		)
	}

	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Opaque != "" ||
		parsed.Host == "" || parsed.User != nil || strings.Contains(trimmed, `\`) {
		return invalid()
	}
	escapedPath := parsed.EscapedPath()
	if encodedPathSeparatorRegex.MatchString(escapedPath) {
		return invalid()
	}

	// Compare the full authority so ports and an empty port delimiter cannot
	// hide behind Hostname/Port normalization.
	host := strings.ToLower(parsed.Host)
	switch host {
	case "imdb.com", "www.imdb.com":
		match := imdbMoviePathRegex.FindStringSubmatch(escapedPath)
		if match == nil {
			return invalid()
		}
		imdbID := strings.ToLower(match[1])
		return domain.MovieIdentityTarget{IMDbID: &imdbID}, nil
	case "themoviedb.org", "www.themoviedb.org":
		match := tmdbMoviePathRegex.FindStringSubmatch(escapedPath)
		if match == nil {
			return invalid()
		}
		parsedTMDBID, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || parsedTMDBID <= 0 || parsedTMDBID > maxSafeInteger ||
			parsedTMDBID > int64(^uint(0)>>1) {
			return invalid()
		}
		tmdbID := int(parsedTMDBID)
		return domain.MovieIdentityTarget{TMDBID: &tmdbID}, nil
	default:
		return invalid()
	}
}
