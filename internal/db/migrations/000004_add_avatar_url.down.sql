-- SQLite < 3.35 lacks DROP COLUMN; both backends should handle it in practice,
-- but for compatibility we rebuild the table the portable way.
CREATE TABLE users_new (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    email        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO users_new (id, display_name, email, created_at)
    SELECT id, display_name, email, created_at FROM users;
DROP TABLE users;
ALTER TABLE users_new RENAME TO users;
