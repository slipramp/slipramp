# PostgreSQL migrations

Add sequential Goose SQL migrations here as the durable model is introduced.
Each migration must include both `-- +goose Up` and `-- +goose Down` sections.
The Server only applies forward migrations at startup; rollback remains an
operator-controlled action.
