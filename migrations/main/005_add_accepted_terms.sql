-- +goose Up
-- +goose StatementBegin

ALTER TABLE organizations
    ADD COLUMN accepted_terms_at TIMESTAMPTZ;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE organizations
    DROP COLUMN IF EXISTS accepted_terms_at;

-- +goose StatementEnd
