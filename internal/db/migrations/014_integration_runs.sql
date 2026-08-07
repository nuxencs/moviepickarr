-- 014: durable progress and history for typed integration work.

CREATE TABLE integration_runs (
    id              INTEGER PRIMARY KEY,
    integration     TEXT    NOT NULL,
    operation       TEXT    NOT NULL,
    trigger         TEXT    NOT NULL,
    initiated_by    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    config_revision INTEGER NOT NULL,
    status          TEXT    NOT NULL CHECK (status IN (
        'running',
        'completed',
        'completed_with_errors',
        'failed',
        'cancelled',
        'interrupted'
    )),
    started_at      INTEGER NOT NULL,
    finished_at     INTEGER,
    total           INTEGER NOT NULL DEFAULT 0 CHECK (total >= 0),
    processed       INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),
    succeeded       INTEGER NOT NULL DEFAULT 0 CHECK (succeeded >= 0),
    failed          INTEGER NOT NULL DEFAULT 0 CHECK (failed >= 0),
    skipped         INTEGER NOT NULL DEFAULT 0 CHECK (skipped >= 0),
    remaining       INTEGER NOT NULL DEFAULT 0 CHECK (remaining >= 0),
    error_summary   TEXT    NOT NULL DEFAULT '',
    failed_subjects TEXT    NOT NULL DEFAULT '[]' CHECK (
        json_valid(failed_subjects) AND json_type(failed_subjects) = 'array'
    ),
    CHECK (length(trim(integration)) > 0),
    CHECK (length(trim(operation)) > 0),
    CHECK (length(trim(trigger)) > 0),
    CHECK (config_revision >= 0),
    CHECK ((status = 'running') = (finished_at IS NULL))
) STRICT;

CREATE INDEX integration_runs_history_index
    ON integration_runs (started_at DESC, id DESC);

CREATE INDEX integration_runs_integration_history_index
    ON integration_runs (integration, started_at DESC, id DESC);

CREATE INDEX integration_runs_operation_history_index
    ON integration_runs (operation, started_at DESC, id DESC);

CREATE INDEX integration_runs_status_history_index
    ON integration_runs (status, started_at DESC, id DESC);

CREATE INDEX integration_runs_trigger_history_index
    ON integration_runs (trigger, started_at DESC, id DESC);

CREATE INDEX integration_runs_current_index
    ON integration_runs (integration, started_at DESC, id DESC)
    WHERE status = 'running';

CREATE INDEX integration_runs_library_current_index
    ON integration_runs (integration, started_at DESC, id DESC)
    WHERE status = 'running'
      AND operation IN ('refresh_stale', 're_enrich_all');
