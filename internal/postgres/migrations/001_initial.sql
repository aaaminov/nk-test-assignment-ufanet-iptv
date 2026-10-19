CREATE TABLE users (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    login text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    active boolean NOT NULL DEFAULT true
);

CREATE TABLE channels (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    number integer NOT NULL CHECK (number > 0),
    name text NOT NULL,
    stream_url text NOT NULL,
    active boolean NOT NULL DEFAULT true
);
