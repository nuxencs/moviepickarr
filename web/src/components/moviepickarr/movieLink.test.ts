import { describe, expect, it } from "vitest";

import { isMovieLink } from "@/components/moviepickarr/movieLink";

describe("isMovieLink", () => {
  it.each([
    "https://www.imdb.com/title/tt0133093/",
    "HTTPS://www.imdb.com/title/tt0133093/",
    "https://imdb.com/title/TT0133093?ref_=fn_all_ttl_1",
    "https://www.imdb.com/title/tt0133093/?next=%2Ffoo#jump",
    "https://www.themoviedb.org/movie/603",
    "https://themoviedb.org/movie/603-the-matrix?language=en-US",
  ])("accepts a movie identity URL: %s", (link) => {
    expect(isMovieLink(link)).toBe(true);
  });

  it.each([
    "",
    "//www.imdb.com/title/tt0133093/",
    "https://example.com/title/tt0133093/",
    "https://www.imdb.com.evil.test/title/tt0133093/",
    "http://www.imdb.com/title/tt0133093/",
    "https://user@www.imdb.com/title/tt0133093/",
    "https://www.imdb.com:443/title/tt0133093/",
    "https://www.imdb.com:/title/tt0133093/",
    "https://www.imdb.com/name/tt0133093/",
    "https://www.imdb.com/title/tt0133093/reviews",
    "https://www.imdb.com/title/x/../tt0133093/",
    "https://www.imdb.com/%2e%2e/title/tt0133093/",
    "https://www.imdb.com/\ttitle/tt0133093/",
    "https://www.imdb.com/\ntitle/tt0133093/",
    "https://www.themoviedb.org/person/603",
    "https://www.themoviedb.org/movie/0",
    "https://www.themoviedb.org/movie/603-the%2Fmatrix",
    "https://www.themoviedb.org/movie/603-the%5Cmatrix",
    "https://www.themoviedb.org/movie/9007199254740992",
    "javascript:alert(1)",
  ])("rejects a non-movie URL: %s", (link) => {
    expect(isMovieLink(link)).toBe(false);
  });
});
