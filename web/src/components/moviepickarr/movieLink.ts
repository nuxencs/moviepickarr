const exactMovieURL =
  /^https:\/\/((?:www\.)?(?:imdb\.com|themoviedb\.org))(\/[^?#]*)(?:\?[^#]*)?(?:#.*)?$/i;
const imdbMoviePath = /^\/title\/tt\d{7,8}\/?$/i;
const tmdbMoviePath = /^\/movie\/([1-9]\d*)(?:-[^/\\]+)?\/?$/;
const encodedPathSeparator = /%2f|%5c/i;

function hasControlCharacter(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code <= 0x1f || code === 0x7f) {
      return true;
    }
  }
  return false;
}

export function isMovieLink(value: string): boolean {
  const trimmed = value.trim();
  const rawMatch = exactMovieURL.exec(trimmed);
  if (
    !rawMatch ||
    trimmed.includes("\\") ||
    hasControlCharacter(trimmed) ||
    encodedPathSeparator.test(rawMatch[2])
  ) {
    return false;
  }

  try {
    const parsed = new URL(trimmed);
    if (
      parsed.protocol !== "https:" ||
      parsed.username ||
      parsed.password ||
      parsed.port
    ) {
      return false;
    }

    const host = rawMatch[1].toLowerCase();
    const path = rawMatch[2];
    if (host === "imdb.com" || host === "www.imdb.com") {
      return imdbMoviePath.test(path);
    }
    if (host === "themoviedb.org" || host === "www.themoviedb.org") {
      const match = tmdbMoviePath.exec(path);
      if (!match) {
        return false;
      }
      const id = Number(match[1]);
      return Number.isSafeInteger(id) && id > 0;
    }
    return false;
  } catch {
    return false;
  }
}
