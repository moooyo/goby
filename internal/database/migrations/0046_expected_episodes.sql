-- Explicit administrator-imported episode facts are independent from physical
-- catalog items. Removing media does not remove the last accepted roster.
CREATE TABLE series_episode_rosters (
    series_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision > 0),
    state text NOT NULL CHECK (state IN ('active', 'withdrawn')),
    source_key text NOT NULL CHECK (octet_length(source_key) BETWEEN 1 AND 128),
    source_label text NOT NULL CHECK (octet_length(source_label) <= 256),
    source_revision text NOT NULL CHECK (octet_length(source_revision) BETWEEN 1 AND 128),
    parser_version integer NOT NULL CHECK (parser_version = 1),
    payload_sha256 text NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    last_edited_by text NOT NULL CHECK (octet_length(last_edited_by) BETWEEN 1 AND 256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- Each accepted replacement or withdrawal retains its immutable normalized
-- payload. A source revision cannot later identify different source content.
CREATE TABLE episode_roster_imports (
    series_id text NOT NULL REFERENCES series_episode_rosters(series_id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision > 0),
    action text NOT NULL CHECK (action IN ('replace', 'withdraw')),
    source_key text NOT NULL CHECK (octet_length(source_key) BETWEEN 1 AND 128),
    source_label text NOT NULL CHECK (octet_length(source_label) <= 256),
    source_revision text NOT NULL CHECK (octet_length(source_revision) BETWEEN 1 AND 128),
    parser_version integer NOT NULL CHECK (parser_version = 1),
    payload bytea NOT NULL CHECK (octet_length(payload) BETWEEN 1 AND 524288),
    payload_sha256 text NOT NULL CHECK (payload_sha256 ~ '^[0-9a-f]{64}$'),
    actor_id text NOT NULL CHECK (octet_length(actor_id) BETWEEN 1 AND 256),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (series_id, revision)
);
CREATE INDEX episode_roster_imports_source_idx
    ON episode_roster_imports(series_id, source_key, source_revision);

CREATE TABLE expected_episodes (
    id text PRIMARY KEY CHECK (id ~ '^missing-[0-9a-f]{32}$'),
    series_id text NOT NULL REFERENCES series_episode_rosters(series_id) ON DELETE CASCADE,
    source_key text NOT NULL CHECK (octet_length(source_key) BETWEEN 1 AND 128),
    entry_key text NOT NULL CHECK (octet_length(entry_key) BETWEEN 1 AND 128),
    season_number integer NOT NULL CHECK (season_number BETWEEN 0 AND 9999),
    episode_number integer NOT NULL CHECK (episode_number BETWEEN 0 AND 9999),
    name text NOT NULL CHECK (octet_length(name) <= 512),
    premiere_date date CHECK (premiere_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
    active boolean NOT NULL,
    import_revision bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    retired_at timestamptz,
    UNIQUE (series_id, source_key, entry_key),
    FOREIGN KEY (series_id, import_revision) REFERENCES episode_roster_imports(series_id, revision),
    CHECK (active = (retired_at IS NULL))
);
CREATE UNIQUE INDEX expected_episodes_active_number_idx
    ON expected_episodes(series_id, season_number, episode_number) WHERE active;
CREATE INDEX expected_episodes_active_series_idx ON expected_episodes(series_id, id) WHERE active;
CREATE INDEX expected_episodes_import_idx ON expected_episodes(series_id, import_revision, id);
