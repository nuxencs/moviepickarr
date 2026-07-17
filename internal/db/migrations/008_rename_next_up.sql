-- 008: rename next_picker to next_up.
--
-- Terminology change only: the rotation role is "next up". A plain RENAME
-- keeps the row, its data, and the FK to users(id) intact — no rebuild, so no
-- fk_off marker is needed (a table rename does not drop or re-point the FK).
ALTER TABLE next_picker RENAME TO next_up;
