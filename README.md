<h1 align="center">moviepickarr</h1>

<p align="center">A self-hosted app for choosing your next movie together.</p>

<div align="center">
  <a href="docs/SCREENSHOTS.md">
    <img src="docs/assets/movies.webp" alt="The Movies tab with the current draw, shared pool, and watched library" width="100%">
  </a>
</div>

## Documentation

- [Installation](docs/INSTALL.md): run with Docker or from source and configure
  your instance.
- [Screenshot gallery](docs/SCREENSHOTS.md): explore the Movies, Members, movie
  details, and Stats views.
- [Administration and integrations](docs/admin-integrations.md): configure TMDB
  and Radarr, test connections, and review integration activity.
- [Operations and recovery](docs/RUNBOOK.md): authentication, reverse proxy
  changes, and integration-key recovery.
- [Development](docs/DEVELOPMENT.md): developer setup, tests, and the tech stack.
- [Product](docs/PRODUCT.md) and [design](docs/DESIGN.md): the decisions behind
  the app.

## Features

- **Personal stashes**: search TMDB and keep your own list of movies to watch.
- **Shared pool**: promote up to three movies from your stash into the group's
  pool for the next draw.
- **Random draws**: take turns drawing the next movie from the pool, with an
  animated reel that reveals the result.
- **Watched library**: keep a shared watch history with dates and who added each
  movie. Browse posters or a list, and search by title or member.
- **Movie details**: posters, backdrops, overviews, runtime, ratings, genres,
  credits, and cast from TMDB.
- **Watch stats**: explore member counts, watch activity, top genres, release
  decades, and most-watched directors and actors. Filter by time range, genre,
  release year, member, actors, or crew.
- **Group administration**: manage members and invites, lock the pool, configure
  integrations, and review run history.
- **Private by design**: host it yourself and invite your friends. No public
  signup.

## Credits

<a href="https://www.themoviedb.org/">
  <img src="web/public/tmdb-logo.svg" alt="TMDB" width="180">
</a>

This product uses the TMDB API but is not endorsed or certified by TMDB.
