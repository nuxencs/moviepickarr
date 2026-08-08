-- 016: durable Radarr configuration, acquisition, and webhook state.

CREATE TABLE radarr_instances (
    id                    INTEGER PRIMARY KEY,
    name                  TEXT    NOT NULL COLLATE NOCASE,
    base_url              TEXT    NOT NULL,
    encrypted_api_key     BLOB    NOT NULL,
    revision              INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    state                 TEXT    NOT NULL DEFAULT 'connected' CHECK (
        state IN ('connected', 'offline', 'credential_unavailable')
    ),
    state_reason          TEXT    NOT NULL DEFAULT '',
    last_checked_at       INTEGER NOT NULL,
    created_at            INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at            INTEGER NOT NULL DEFAULT (unixepoch()),
    archived_at           INTEGER,
    CHECK (length(trim(name)) > 0),
    CHECK (length(trim(base_url)) > 0),
    CHECK (length(encrypted_api_key) > 0),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
) STRICT;

CREATE UNIQUE INDEX radarr_instances_active_name_unique
    ON radarr_instances (name COLLATE NOCASE)
    WHERE archived_at IS NULL;

CREATE TABLE radarr_presets (
    id                     INTEGER PRIMARY KEY,
    name                   TEXT    NOT NULL COLLATE NOCASE,
    instance_id            INTEGER NOT NULL REFERENCES radarr_instances (id) ON DELETE RESTRICT,
    root_folder_id         INTEGER NOT NULL CHECK (root_folder_id > 0),
    root_folder_path       TEXT    NOT NULL,
    quality_profile_id     INTEGER NOT NULL CHECK (quality_profile_id > 0),
    quality_profile_name   TEXT    NOT NULL,
    tags                   TEXT    NOT NULL DEFAULT '[]' CHECK (
        json_valid(tags) AND json_type(tags) = 'array'
    ),
    minimum_availability   TEXT    NOT NULL CHECK (
        minimum_availability IN ('tba', 'announced', 'inCinemas', 'released')
    ),
    acquisition_mode       TEXT    NOT NULL CHECK (
        acquisition_mode IN ('manual', 'automatic')
    ),
    revision               INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    valid                  INTEGER NOT NULL CHECK (valid IN (0, 1)),
    validation_reason      TEXT    NOT NULL DEFAULT '',
    validated_at           INTEGER NOT NULL,
    created_at             INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at             INTEGER NOT NULL DEFAULT (unixepoch()),
    archived_at            INTEGER,
    CHECK (length(trim(name)) > 0),
    CHECK (length(trim(root_folder_path)) > 0),
    CHECK (length(trim(quality_profile_name)) > 0),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
) STRICT;

CREATE UNIQUE INDEX radarr_presets_active_name_unique
    ON radarr_presets (name COLLATE NOCASE)
    WHERE archived_at IS NULL;

CREATE INDEX radarr_presets_instance_active_index
    ON radarr_presets (instance_id, name COLLATE NOCASE)
    WHERE archived_at IS NULL;

CREATE TABLE radarr_acquisitions (
    id                              INTEGER PRIMARY KEY,
    movie_id                        INTEGER NOT NULL REFERENCES movies (id) ON DELETE CASCADE,
    revision                        INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    status                          TEXT    NOT NULL DEFAULT 'needs_preset' CHECK (
        status IN (
            'needs_preset',
            'needs_release',
            'waiting_for_radarr',
            'queued',
            'downloading',
            'importing',
            'downloaded',
            'action_needed',
            'abandoned'
        )
    ),
    action_reason                   TEXT CHECK (
        action_reason IS NULL OR action_reason IN (
            'preset_required',
            'identity_required',
            'release_required',
            'configuration_invalid',
            'connection_failed',
            'add_failed',
            'no_releases',
            'release_failed',
            'import_failed',
            'monitoring_failed'
        )
    ),
    action_version                  INTEGER NOT NULL DEFAULT 0 CHECK (action_version >= 0),
    action_started_at               INTEGER,

    movie_title                     TEXT    NOT NULL,
    movie_year                      INTEGER CHECK (
        movie_year IS NULL OR movie_year BETWEEN 1870 AND 2100
    ),
    tmdb_id                         INTEGER CHECK (tmdb_id IS NULL OR tmdb_id > 0),
    imdb_id                         TEXT,
    identity_source                 TEXT CHECK (
        identity_source IS NULL OR identity_source IN ('tmdb', 'imdb', 'override')
    ),
    identity_override_tmdb_id       INTEGER CHECK (
        identity_override_tmdb_id IS NULL OR identity_override_tmdb_id > 0
    ),

    drawn_at                        INTEGER NOT NULL,
    reveal_at                       INTEGER NOT NULL,
    draw_client_id                  TEXT    NOT NULL DEFAULT '',
    revealed_at                     INTEGER,

    preset_id                       INTEGER REFERENCES radarr_presets (id) ON DELETE RESTRICT,
    preset_name                     TEXT,
    target_instance_id              INTEGER REFERENCES radarr_instances (id) ON DELETE RESTRICT,
    target_instance_name            TEXT,
    target_root_folder_id           INTEGER CHECK (
        target_root_folder_id IS NULL OR target_root_folder_id > 0
    ),
    target_root_folder_path         TEXT,
    target_quality_profile_id       INTEGER CHECK (
        target_quality_profile_id IS NULL OR target_quality_profile_id > 0
    ),
    target_quality_profile_name     TEXT,
    target_tags                     TEXT    NOT NULL DEFAULT '[]' CHECK (
        json_valid(target_tags) AND json_type(target_tags) = 'array'
    ),
    target_minimum_availability     TEXT CHECK (
        target_minimum_availability IS NULL OR
        target_minimum_availability IN ('tba', 'announced', 'inCinemas', 'released')
    ),
    target_acquisition_mode         TEXT CHECK (
        target_acquisition_mode IS NULL OR target_acquisition_mode IN ('manual', 'automatic')
    ),
    target_selected_at              INTEGER,
    target_selected_by              INTEGER REFERENCES users (id) ON DELETE SET NULL,
    target_locked_at                INTEGER,
    target_locked_by                INTEGER REFERENCES users (id) ON DELETE SET NULL,
    radarr_movie_id                 INTEGER CHECK (radarr_movie_id IS NULL OR radarr_movie_id > 0),
    adopted_existing                INTEGER NOT NULL DEFAULT 0 CHECK (adopted_existing IN (0, 1)),
    effective_configuration         TEXT    NOT NULL DEFAULT '{}' CHECK (
        json_valid(effective_configuration) AND json_type(effective_configuration) = 'object'
    ),
    target_preview_existing         INTEGER NOT NULL DEFAULT 0 CHECK (
        target_preview_existing IN (0, 1)
    ),
    target_previewed_at             INTEGER,
    mutation_state                  TEXT    NOT NULL DEFAULT 'idle' CHECK (
        mutation_state IN ('idle', 'adding', 'checking_replacement', 'recreating', 'searching', 'grabbing')
    ),
    automatic_search_claimed_at     INTEGER,
    automatic_search_command_id     INTEGER CHECK (
        automatic_search_command_id IS NULL OR automatic_search_command_id > 0
    ),
    automatic_search_completed_at   INTEGER,

    latest_release_title            TEXT,
    latest_release_quality          TEXT,
    latest_release_selected_at      INTEGER,
    latest_release_selected_by      INTEGER REFERENCES users (id) ON DELETE SET NULL,
    manual_attempt_count            INTEGER NOT NULL DEFAULT 0 CHECK (manual_attempt_count >= 0),
    latest_failure_summary          TEXT,
    latest_failure_at               INTEGER,

    queue_status                    TEXT    NOT NULL DEFAULT 'none' CHECK (
        queue_status IN ('none', 'queued', 'downloading', 'importing', 'failed')
    ),
    queue_summary                   TEXT    NOT NULL DEFAULT '',
    last_checked_at                 INTEGER,
    next_check_at                   INTEGER,

    queued_at                       INTEGER,
    downloading_at                  INTEGER,
    importing_at                    INTEGER,
    downloaded_at                   INTEGER,
    abandoned_at                    INTEGER,
    abandoned_by                    INTEGER REFERENCES users (id) ON DELETE SET NULL,
    abandonment_reason              TEXT,

    created_at                      INTEGER NOT NULL,
    updated_at                      INTEGER NOT NULL,

    CHECK (length(trim(movie_title)) > 0),
    CHECK (imdb_id IS NULL OR imdb_id GLOB 'tt[0-9][0-9][0-9][0-9][0-9][0-9][0-9]*'),
    CHECK (reveal_at >= drawn_at),
    CHECK (revealed_at IS NULL OR revealed_at >= drawn_at),
    CHECK ((status = 'downloaded') = (downloaded_at IS NOT NULL)),
    CHECK ((status = 'abandoned') = (abandoned_at IS NOT NULL)),
    CHECK (status != 'abandoned' OR length(trim(abandonment_reason)) > 0),
    CHECK (
        target_instance_id IS NULL OR (
            preset_name IS NOT NULL AND length(trim(preset_name)) > 0 AND
            target_instance_name IS NOT NULL AND length(trim(target_instance_name)) > 0 AND
            target_root_folder_id IS NOT NULL AND
            target_root_folder_path IS NOT NULL AND length(trim(target_root_folder_path)) > 0 AND
            target_quality_profile_id IS NOT NULL AND
            target_quality_profile_name IS NOT NULL AND length(trim(target_quality_profile_name)) > 0 AND
            target_minimum_availability IS NOT NULL AND
            target_acquisition_mode IS NOT NULL AND
            target_selected_at IS NOT NULL
        )
    ),
    CHECK (target_locked_at IS NULL OR target_instance_id IS NOT NULL),
    CHECK (radarr_movie_id IS NULL OR target_locked_at IS NOT NULL)
) STRICT;

-- All radarr_acquisitions timestamps use Unix milliseconds. Draw choreography
-- has a 16.5-second deadline, so the application's usual epoch-second storage
-- would move the durable Reveal boundary by up to one second after a restart.

CREATE INDEX radarr_acquisitions_movie_history_index
    ON radarr_acquisitions (movie_id, drawn_at DESC, id DESC);

CREATE UNIQUE INDEX radarr_acquisitions_unresolved_movie_unique
    ON radarr_acquisitions (movie_id)
    WHERE status NOT IN ('downloaded', 'abandoned');

CREATE INDEX radarr_acquisitions_admin_active_index
    ON radarr_acquisitions (status, updated_at DESC, id DESC)
    WHERE revealed_at IS NOT NULL AND status NOT IN ('downloaded', 'abandoned');

CREATE INDEX radarr_acquisitions_concealed_current_index
    ON radarr_acquisitions (movie_id, reveal_at, id DESC)
    WHERE revealed_at IS NULL;

CREATE INDEX radarr_acquisitions_target_active_index
    ON radarr_acquisitions (target_instance_id, status)
    WHERE target_instance_id IS NOT NULL AND status NOT IN ('downloaded', 'abandoned');

CREATE TABLE radarr_webhook_destinations (
    id                    INTEGER PRIMARY KEY,
    name                  TEXT    NOT NULL COLLATE NOCASE,
    kind                  TEXT    NOT NULL CHECK (kind IN ('generic', 'discord')),
    encrypted_url         BLOB    NOT NULL,
    reason_filters        TEXT    NOT NULL DEFAULT '[]' CHECK (
        json_valid(reason_filters) AND json_type(reason_filters) = 'array'
    ),
    discord_role_mention  TEXT    NOT NULL DEFAULT '',
    enabled               INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    verified_at           INTEGER,
    revision              INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    health_warning_at     INTEGER,
    health_warning_reason TEXT    NOT NULL DEFAULT '',
    created_at            INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at            INTEGER NOT NULL DEFAULT (unixepoch()),
    archived_at           INTEGER,
    CHECK (length(trim(name)) > 0),
    CHECK (length(encrypted_url) > 0),
    CHECK (enabled = 0 OR (verified_at IS NOT NULL AND archived_at IS NULL)),
    CHECK (archived_at IS NULL OR archived_at >= created_at)
) STRICT;

CREATE UNIQUE INDEX radarr_webhook_destinations_active_name_unique
    ON radarr_webhook_destinations (name COLLATE NOCASE)
    WHERE archived_at IS NULL;

CREATE INDEX radarr_webhook_destinations_enabled_index
    ON radarr_webhook_destinations (id)
    WHERE enabled = 1 AND archived_at IS NULL;

CREATE TABLE radarr_webhook_deliveries (
    id                  INTEGER PRIMARY KEY,
    destination_id      INTEGER NOT NULL REFERENCES radarr_webhook_destinations (id) ON DELETE RESTRICT,
    acquisition_id      INTEGER NOT NULL REFERENCES radarr_acquisitions (id) ON DELETE CASCADE,
    event               TEXT    NOT NULL DEFAULT 'acquisition.action_required' CHECK (
        event = 'acquisition.action_required'
    ),
    reason              TEXT    NOT NULL CHECK (
        reason IN (
            'preset_required',
            'identity_required',
            'release_required',
            'configuration_invalid',
            'connection_failed',
            'add_failed',
            'no_releases',
            'release_failed',
            'import_failed',
            'monitoring_failed'
        )
    ),
    destination_revision INTEGER NOT NULL CHECK (destination_revision > 0),
    action_version      INTEGER NOT NULL CHECK (action_version > 0),
    target_label        TEXT    NOT NULL DEFAULT '',
    status              TEXT    NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'sending', 'delivered', 'terminal_failed', 'superseded')
    ),
    claim_version       INTEGER NOT NULL DEFAULT 0 CHECK (claim_version >= 0),
    claim_expires_at    INTEGER,
    attempt_count       INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at     INTEGER NOT NULL,
    last_attempt_at     INTEGER,
    delivered_at        INTEGER,
    resolved_at         INTEGER,
    error_summary       TEXT    NOT NULL DEFAULT '',
    created_at          INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at          INTEGER NOT NULL DEFAULT (unixepoch()),
    CHECK ((status = 'delivered') = (delivered_at IS NOT NULL)),
    CHECK (status != 'pending' OR delivered_at IS NULL),
    CHECK ((status = 'sending') = (claim_expires_at IS NOT NULL)),
    CHECK (status != 'superseded' OR resolved_at IS NOT NULL)
) STRICT;

CREATE UNIQUE INDEX radarr_webhook_deliveries_condition_unique
    ON radarr_webhook_deliveries (destination_id, acquisition_id, action_version);

CREATE INDEX radarr_webhook_deliveries_pending_index
    ON radarr_webhook_deliveries (next_attempt_at, id)
    WHERE status = 'pending';

CREATE INDEX radarr_webhook_deliveries_claim_expiry_index
    ON radarr_webhook_deliveries (claim_expires_at, id)
    WHERE status = 'sending';

CREATE INDEX radarr_webhook_deliveries_retention_index
    ON radarr_webhook_deliveries (status, delivered_at, resolved_at, id);

-- An action event is useful only while that exact condition remains current.
-- Retire older pending conditions atomically whenever Acquisition state moves.
CREATE TRIGGER radarr_acquisition_supersede_webhook_deliveries
AFTER UPDATE OF status, action_reason, action_version ON radarr_acquisitions
BEGIN
    UPDATE radarr_webhook_deliveries
    SET status = 'superseded',
        claim_expires_at = NULL,
        resolved_at = unixepoch(),
        error_summary = 'action condition resolved',
        updated_at = unixepoch()
    WHERE acquisition_id = NEW.id
      AND status IN ('pending', 'sending')
      AND (
          NEW.revealed_at IS NULL OR
          NEW.status IN ('downloaded', 'abandoned') OR
          NEW.action_reason IS NULL OR
          action_version != NEW.action_version OR
          reason != NEW.action_reason
      );
END;

-- A Current draw that predates Radarr support has already crossed the old
-- process-local Reveal boundary. Backfill it as revealed so an upgrade neither
-- loses its Acquisition nor invents a concealed reel that cannot be resumed.
INSERT INTO radarr_acquisitions (
    movie_id,
    status,
    action_reason,
    action_version,
    action_started_at,
    movie_title,
    movie_year,
    tmdb_id,
    imdb_id,
    identity_source,
    drawn_at,
    reveal_at,
    draw_client_id,
    revealed_at,
    created_at,
    updated_at
)
SELECT
    id,
    'needs_preset',
    'preset_required',
    1,
    unixepoch() * 1000,
    title,
    (
        SELECT CASE
            WHEN substr(mm.release_date, 1, 4) GLOB '[0-9][0-9][0-9][0-9]'
             AND CAST(substr(mm.release_date, 1, 4) AS INTEGER) BETWEEN 1870 AND 2100
            THEN CAST(substr(mm.release_date, 1, 4) AS INTEGER)
            ELSE NULL
        END
        FROM movie_metadata AS mm
        WHERE mm.movie_id = movies.id
    ),
    tmdb_id,
    imdb_id,
    CASE
        WHEN tmdb_id IS NOT NULL THEN 'tmdb'
        WHEN imdb_id IS NOT NULL THEN 'imdb'
        ELSE NULL
    END,
    unixepoch() * 1000,
    unixepoch() * 1000,
    '',
    unixepoch() * 1000,
    unixepoch() * 1000,
    unixepoch() * 1000
FROM movies
WHERE status = 'current';
