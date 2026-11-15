// English only: movies.title is always the TMDB en-US title (ADR 0013).
const LEADING_ARTICLE = /^(?:the|an|a)\s+(?=\S)/i;

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });

const sortKey = (title: string) => title.replace(LEADING_ARTICLE, "");

/** Title order for every movie list: a leading "The", "A" or "An" does not count (#413). */
export function compareTitles(a: string, b: string): number {
  return collator.compare(sortKey(a), sortKey(b)) || collator.compare(a, b);
}
