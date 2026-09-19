-- Managed artwork owns its bytes in PostgreSQL. Owner removal cascades through
-- the set and its payloads; no external file or orphan blob needs collection.
CREATE TABLE artwork_state (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id text UNIQUE REFERENCES items(id) ON DELETE CASCADE,
    entity_id bigint UNIQUE REFERENCES catalog_entities(id) ON DELETE CASCADE,
    user_id text UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    managed_types text[] NOT NULL DEFAULT '{}',
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (num_nonnulls(item_id, entity_id, user_id) = 1),
    CHECK (cardinality(managed_types) <= 11 AND array_position(managed_types, NULL) IS NULL),
    CHECK (managed_types <@ ARRAY['Primary','Backdrop','Thumb','Banner','Logo','Art','Disc','Box','BoxRear','Menu','Screenshot']::text[]),
    CHECK (user_id IS NULL OR managed_types <@ ARRAY['Primary']::text[])
);

CREATE TABLE artwork_images (
    state_id bigint NOT NULL REFERENCES artwork_state(id) ON DELETE CASCADE,
    image_type text NOT NULL CHECK (image_type IN ('Primary','Backdrop','Thumb','Banner','Logo','Art','Disc','Box','BoxRear','Menu','Screenshot')),
    image_index integer NOT NULL CHECK (image_index BETWEEN 0 AND 31),
    content bytea NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 20971520),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg','image/png','image/gif')),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 16384),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 16384),
    source_hash text NOT NULL CHECK (source_hash ~ '^[a-f0-9]{64}$'),
    modified_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (state_id, image_type, image_index),
    CHECK (width::bigint * height <= 26214400),
    CHECK (image_type IN ('Backdrop','Screenshot') OR image_index = 0)
);

-- Entity preferences do not mutate or derive state from associated media.
CREATE TABLE entity_user_data (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity_id bigint NOT NULL REFERENCES catalog_entities(id) ON DELETE CASCADE,
    playback_position_ticks bigint NOT NULL DEFAULT 0 CHECK (playback_position_ticks = 0),
    play_count integer NOT NULL DEFAULT 0 CHECK (play_count >= 0),
    is_favorite boolean NOT NULL DEFAULT false,
    played boolean NOT NULL DEFAULT false,
    last_played_at timestamptz,
    rating double precision CHECK (rating BETWEEN 0 AND 10 AND rating <> 'NaN'::double precision),
    likes boolean,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (user_id, entity_id)
);
CREATE INDEX entity_user_data_entity_idx ON entity_user_data(entity_id);
