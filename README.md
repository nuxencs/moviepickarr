# moviepickarr

moviepickarr is a small, self-hosted web app for a group of friends who share a
movie night. Each member keeps a personal stash of movies, promotes up to 3 of
them into a shared pool, and the app draws the next movie from the pool at
random. Watched movies land in a shared library with stats on top.

It is private by design: you run it yourself and share the link with your
friends. There is no public signup.

## How it works

1. Stash: each member keeps a personal list of movies they want to watch.
   Movies are added via [TMDB](https://www.themoviedb.org/) search, which also
   supplies posters and details.
2. Pool: each member promotes up to 3 movies from their stash into the shared
   pool.
3. Draw: on movie night, the app draws one movie from the pool at random.
4. Watched: mark the current draw as watched and it moves into the watched
   library, together with who added it and the watch date.
5. Stats: watch counts per member, activity by weekday and hour, top genres,
   most-watched directors and actors.

## The tabs

- Movies: the current draw with its poster and details, the button that draws
  the next movie, the shared pool, and the watched library (searchable by title
  or by who added a movie). Every movie opens a detail view with its overview,
  credits and a cast strip.
- Members: everyone in the group, each with their pool slots and a searchable
  stash. New movies are added here.
- Stats: the watch history over a selectable time window. A member leaderboard,
  weekday and hourly activity, hours watched, average rating, top genres,
  most-watched directors and actors, and a release-decades timeline. The whole
  page can be filtered by genre, release year or decade, adder, and specific
  actors or crew.

Movie data comes from TMDB and is fetched in the background: posters,
backdrops, runtimes, ratings, genres, taglines, overviews, cast and crew.
Movies without data yet show a placeholder poster.

## Documentation

- [`docs/INSTALL.md`](docs/INSTALL.md): how to run it, with Docker or from
  source, and all configuration options.
- [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md): developer setup and the tech
  stack.
- [`docs/RUNBOOK.md`](docs/RUNBOOK.md): auth cutover and loosening a forward-auth
  proxy in front of the app.
- [`docs/PRODUCT.md`](docs/PRODUCT.md) and [`docs/DESIGN.md`](docs/DESIGN.md):
  product and design decisions.

---

<sub>This product uses the TMDB API but is not endorsed or certified by TMDB.</sub>
