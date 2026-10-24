-- 020: keep TMDB's original-language title next to the English display title.
--
-- Enrichment owns both titles (ADR 0013). Existing rows stay empty until the
-- next refresh or Re-enrich all fills them.

ALTER TABLE movie_metadata ADD COLUMN original_title TEXT NOT NULL DEFAULT '';
