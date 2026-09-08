CREATE TABLE item_images (
    item_id text NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    root_id text NOT NULL REFERENCES library_roots(id) ON DELETE CASCADE,
    image_type text NOT NULL CHECK (image_type IN ('Primary', 'Backdrop', 'Thumb', 'Banner', 'Logo', 'Art')),
    image_index integer NOT NULL CHECK (image_index BETWEEN 0 AND 31),
    relative_path text NOT NULL CHECK (
        relative_path <> '' AND relative_path NOT LIKE '/%'
        AND position(chr(92) IN relative_path) = 0
        AND relative_path !~ '(^|/)[.][.](/|$)'
    ),
    file_identity text NOT NULL,
    source_hash text NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
    file_size bigint NOT NULL CHECK (file_size BETWEEN 0 AND 20971520),
    modified_at timestamptz NOT NULL,
    width integer NOT NULL CHECK (width BETWEEN 1 AND 16384),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 16384),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/gif')),
    PRIMARY KEY (item_id, image_type, image_index),
    CHECK (image_type = 'Backdrop' OR image_index = 0),
    CHECK (width::bigint * height::bigint <= 26214400)
);

CREATE INDEX item_images_root_idx ON item_images(root_id);
