/* ============================================================
   Movie Gang — shared helpers: TMDB image URLs, procedural
   poster/backdrop art, deterministic hues, formatting.
   ============================================================ */

import type { Movie } from "@/types/Response";

const TMDB_IMG = "https://image.tmdb.org/t/p";

/** Full TMDB poster URL from a raw poster_path, or null if absent. */
export function posterUrl(path?: string | null, size: "w342" | "w500" = "w342"): string | null {
  return path ? `${TMDB_IMG}/${size}${path}` : null;
}

/** Full TMDB backdrop URL from a raw backdrop_path, or null if absent. */
export function backdropUrl(path?: string | null, size: "w1280" | "w780" = "w1280"): string | null {
  return path ? `${TMDB_IMG}/${size}${path}` : null;
}

/** Deterministic 0..360 hue from a string (stable per title). */
export function hueOf(str: string): number {
  let h = 0;
  for (let i = 0; i < str.length; i++) h = (h * 31 + str.charCodeAt(i)) % 360;
  return h;
}

/** Procedural duotone "alt-poster" gradient (2:3) — used when no real poster. */
export function posterBg(hue: number): string {
  const h2 = (hue + 28) % 360;
  return [
    `radial-gradient(120% 90% at 78% 14%, hsl(${h2} 70% 32% / 0.55), transparent 60%)`,
    `radial-gradient(150% 120% at 12% 100%, hsl(${(hue + 200) % 360} 45% 10%), transparent 70%)`,
    `linear-gradient(158deg, hsl(${hue} 52% 26%) 0%, hsl(${hue} 60% 12%) 55%, hsl(${(hue + 18) % 360} 65% 7%) 100%)`,
  ].join(", ");
}

/** Wide cinematic backdrop gradient (16:9) — used when no real backdrop. */
export function backdropBg(hue: number): string {
  const h2 = (hue + 34) % 360;
  return [
    `radial-gradient(80% 140% at 82% 30%, hsl(${h2} 72% 38% / 0.65), transparent 62%)`,
    `radial-gradient(90% 160% at 8% 80%, hsl(${(hue + 210) % 360} 50% 14% / 0.9), transparent 60%)`,
    `linear-gradient(110deg, hsl(${hue} 48% 20%) 0%, hsl(${hue} 58% 9%) 60%, hsl(${(hue + 20) % 360} 64% 6%) 100%)`,
  ].join(", ");
}

/** "1 movie" / "3 movies" — count plus a count-aware noun (default plural adds "s"). */
export function plural(count: number, noun: string, pluralForm?: string): string {
  return `${count} ${count === 1 ? noun : pluralForm ?? `${noun}s`}`;
}

/** Up-to-two-letter initials from a name, e.g. "Hauptmann Schubert" -> "HS". */
export function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

/** Diagonal hue-derived gradient used for square avatars. */
export function avatarBg(hue: number): string {
  return `linear-gradient(150deg, hsl(${hue} 55% 48%), hsl(${(hue + 30) % 360} 60% 32%))`;
}

/** Release year from a TMDB release_date ("YYYY-MM-DD"), or undefined. */
export function yearOf(releaseDate?: string): number | undefined {
  if (!releaseDate) return undefined;
  const y = parseInt(releaseDate.slice(0, 4), 10);
  return Number.isNaN(y) ? undefined : y;
}

/** "2h 16m" / "92m" from a runtime in minutes, or undefined if 0/absent. */
export function runtimeLabel(runtime?: number): string | undefined {
  if (!runtime || runtime <= 0) return undefined;
  const h = Math.floor(runtime / 60);
  const m = runtime % 60;
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

/** Compact relative date, e.g. "today", "3d ago", "May 17". */
export function relativeDate(iso?: string): string {
  if (!iso) return "";
  const then = new Date(iso);
  const now = new Date();
  const days = Math.floor((now.getTime() - then.getTime()) / 86_400_000);
  if (days <= 0) return "today";
  if (days === 1) return "yesterday";
  if (days < 7) return `${days}d ago`;
  return then.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

const MON_SHORT = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** "Mon D" date + "HH:MM" time, for the watched-list right column. */
export function dateTimeParts(iso?: string): { date: string; time: string } {
  if (!iso) return { date: "", time: "" };
  const d = new Date(iso);
  const date = `${MON_SHORT[d.getMonth()]} ${d.getDate()}`;
  const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
  return { date, time };
}

/** Rounded TMDB rating ("8.2") or undefined when unrated. */
export function ratingLabel(voteAverage?: number): string | undefined {
  if (!voteAverage || voteAverage <= 0) return undefined;
  return voteAverage.toFixed(1);
}

/** Convenience: the hue for a movie (prefers title for stable art). */
export function movieHue(movie: Pick<Movie, "title">): number {
  return hueOf(movie.title);
}

/**
 * External links for a movie, derived from its stable ids. Letterboxd resolves
 * via /tmdb/{id} (preferred) or /imdb/{id}. Only links with a backing id are
 * returned, in a stable order.
 */
export function externalLinks(movie: Pick<Movie, "tmdbId" | "imdbId">): { label: string; href: string }[] {
  const links: { label: string; href: string }[] = [];
  if (movie.imdbId) links.push({ label: "IMDb", href: `https://www.imdb.com/title/${movie.imdbId}/` });
  if (movie.tmdbId) links.push({ label: "TMDB", href: `https://www.themoviedb.org/movie/${movie.tmdbId}` });
  if (movie.tmdbId) {
    links.push({ label: "Letterboxd", href: `https://letterboxd.com/tmdb/${movie.tmdbId}` });
  } else if (movie.imdbId) {
    links.push({ label: "Letterboxd", href: `https://letterboxd.com/imdb/${movie.imdbId}` });
  }
  return links;
}
