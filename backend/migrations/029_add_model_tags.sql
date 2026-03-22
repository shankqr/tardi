-- +goose Up
ALTER TABLE models ADD COLUMN tags TEXT[] NOT NULL DEFAULT '{}';

UPDATE models SET tags = '{Best Free}' WHERE id = 'nvidia/nemotron-3-super-120b-a12b:free';
UPDATE models SET tags = '{Best Quality}' WHERE id = 'anthropic/claude-opus-4.6';
UPDATE models SET tags = '{Best Value}' WHERE id = 'xiaomi/mimo-v2-pro';
UPDATE models SET tags = '{Best Value}' WHERE id = 'minimax/minimax-m2.7';

-- +goose Down
ALTER TABLE models DROP COLUMN tags;
