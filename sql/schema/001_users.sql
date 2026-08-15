-- +goose Up
CREATE TABLE users (
    id UUID,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    email TEXT NOT NULL,
    PRIMARY KEY (id),
    UNIQUE (email)
);

-- +goose Down
DROP TABLE users;
