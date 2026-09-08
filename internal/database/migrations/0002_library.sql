CREATE TABLE libraries (
    id text PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 128),
    collection_type text NOT NULL CHECK (collection_type IN ('movies', 'tvshows', 'music', 'mixed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    last_scan_at timestamptz
);

CREATE TABLE library_roots (
    id text PRIMARY KEY,
    library_id text NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path text NOT NULL,
    allowed_path text NOT NULL,
    relative_path text NOT NULL,
    UNIQUE (library_id, path)
);

CREATE TABLE items (
    id text PRIMARY KEY,
    library_id text NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    root_id text REFERENCES library_roots(id) ON DELETE CASCADE,
    parent_id text REFERENCES items(id) ON DELETE CASCADE,
    name text NOT NULL,
    sort_name text NOT NULL,
    type text NOT NULL,
    path text NOT NULL DEFAULT '',
    relative_path text NOT NULL DEFAULT '',
    overview text NOT NULL DEFAULT '',
    is_folder boolean NOT NULL DEFAULT false,
    index_number integer NOT NULL DEFAULT 0,
    parent_index_number integer NOT NULL DEFAULT 0,
    media jsonb,
    file_identity text NOT NULL DEFAULT '',
    file_size bigint NOT NULL DEFAULT 0,
    modified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (root_id, relative_path)
);

CREATE INDEX items_library_parent_idx ON items(library_id, parent_id);
CREATE INDEX items_parent_idx ON items(parent_id);
CREATE INDEX items_sort_idx ON items(lower(sort_name), id);
CREATE INDEX items_file_identity_idx ON items(library_id, file_identity) WHERE file_identity <> '';

CREATE TABLE scan_jobs (
    id text PRIMARY KEY,
    library_id text NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    status text NOT NULL CHECK (status IN ('Queued', 'Running', 'Completed', 'Failed', 'Cancelled', 'Interrupted')),
    error text NOT NULL DEFAULT '',
    scanned integer NOT NULL DEFAULT 0,
    added integer NOT NULL DEFAULT 0,
    updated integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz,
    cancel_requested boolean NOT NULL DEFAULT false
);

CREATE UNIQUE INDEX scan_jobs_one_active_library_idx ON scan_jobs(library_id)
    WHERE status IN ('Queued', 'Running');
CREATE INDEX scan_jobs_created_idx ON scan_jobs(created_at DESC, id);
