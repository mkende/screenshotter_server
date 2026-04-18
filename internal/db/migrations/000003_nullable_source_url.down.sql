CREATE TABLE images_new (
    id         TEXT PRIMARY KEY,
    owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    source_url TEXT NOT NULL DEFAULT '',
    file_path  TEXT NOT NULL,
    title      TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO images_new SELECT id, owner_id, COALESCE(source_url, ''), file_path, title, created_at FROM images;
DROP TABLE images;
ALTER TABLE images_new RENAME TO images;
CREATE INDEX IF NOT EXISTS images_owner_created ON images(owner_id, created_at DESC);
