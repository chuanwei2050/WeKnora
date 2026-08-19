ALTER TABLE users
    ADD COLUMN IF NOT EXISTS nickname VARCHAR(100) NOT NULL DEFAULT '';

UPDATE users
SET nickname = username
WHERE nickname = '';
