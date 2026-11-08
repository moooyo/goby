-- Display ordering is independent from physical media numbering and identity.
-- A provider group is selected explicitly; no group is inferred from its name.
CREATE TABLE series_display_orders (
    series_id text PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
    revision bigint NOT NULL CHECK (revision > 0),
    display_order text NOT NULL CHECK (display_order IN ('Aired', 'Dvd', 'Absolute')),
    provider_series_id text NOT NULL DEFAULT '' CHECK (octet_length(provider_series_id) <= 32),
    provider_group_id text NOT NULL DEFAULT '' CHECK (octet_length(provider_group_id) <= 128),
    group_name text NOT NULL DEFAULT '' CHECK (octet_length(group_name) <= 512),
    mapping_sha256 text NOT NULL DEFAULT '' CHECK (mapping_sha256 = '' OR mapping_sha256 ~ '^[0-9a-f]{64}$'),
    entry_count integer NOT NULL CHECK (entry_count BETWEEN 0 AND 2000),
    last_edited_by text NOT NULL CHECK (octet_length(last_edited_by) BETWEEN 1 AND 256),
    fetched_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK ((display_order = 'Aired' AND provider_series_id = '' AND provider_group_id = '' AND entry_count = 0 AND mapping_sha256 = '' AND fetched_at IS NULL)
        OR (display_order <> 'Aired' AND provider_series_id ~ '^[1-9][0-9]{0,18}$' AND provider_group_id <> '' AND entry_count > 0 AND mapping_sha256 <> '' AND fetched_at IS NOT NULL))
);

CREATE TABLE series_display_order_entries (
    series_id text NOT NULL REFERENCES series_display_orders(series_id) ON DELETE CASCADE,
    season_number integer NOT NULL CHECK (season_number BETWEEN 0 AND 9999),
    episode_number integer NOT NULL CHECK (episode_number BETWEEN 0 AND 9999),
    group_order integer NOT NULL CHECK (group_order BETWEEN 0 AND 2000),
    episode_order integer NOT NULL CHECK (episode_order BETWEEN 0 AND 2000),
    ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 1999),
    group_name text NOT NULL CHECK (octet_length(group_name) <= 512),
    provider_episode_id text NOT NULL CHECK (provider_episode_id ~ '^[1-9][0-9]{0,18}$'),
    PRIMARY KEY (series_id, season_number, episode_number),
    UNIQUE (series_id, ordinal),
    UNIQUE (series_id, group_order, episode_order),
    UNIQUE (series_id, provider_episode_id)
);
