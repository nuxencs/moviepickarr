-- 019: keep database turn eligibility in one schema object.
--
-- Repository queries use this view instead of repeating the role set. A future
-- eligible role therefore needs one schema change, not edits across each turn
-- read and handoff query.

CREATE VIEW turn_participants AS
SELECT id, name, created_at, updated_at
FROM users
WHERE archived_at IS NULL
  AND role IN ('member', 'admin');
