-- +goose Up
-- +goose StatementBegin
CREATE TABLE preview_terms_acceptances (
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    terms_version TEXT NOT NULL CHECK (length(terms_version) BETWEEN 1 AND 64),
    accepted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, terms_version)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS preview_terms_acceptances;
-- +goose StatementEnd
