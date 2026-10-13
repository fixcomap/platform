-- Datos mínimos por diseño: los jugadores pueden ser menores, así que solo
-- nombre, apellido, apodo y dorsal. Nada de fechas de nacimiento ni fotos.

CREATE TABLE teams (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- El acceso se decide por email de Google: quien no está aquí (ni es admin
-- en APP_CONFIG) no ve el equipo.
CREATE TABLE team_coaches (
    team_id bigint NOT NULL REFERENCES teams ON DELETE CASCADE,
    email   text   NOT NULL,
    PRIMARY KEY (team_id, email)
);
CREATE INDEX team_coaches_email ON team_coaches (email);

CREATE TABLE players (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    team_id    bigint  NOT NULL REFERENCES teams ON DELETE CASCADE,
    first_name text    NOT NULL,
    last_name  text    NOT NULL DEFAULT '',
    nickname   text    NOT NULL DEFAULT '',
    number     integer,
    -- Baja lógica: un jugador que se va conserva su historial de asistencia.
    active     boolean NOT NULL DEFAULT true
);
CREATE INDEX players_team ON players (team_id);

CREATE TABLE trainings (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    team_id bigint NOT NULL REFERENCES teams ON DELETE CASCADE,
    day     date   NOT NULL,
    notes   text   NOT NULL DEFAULT ''
);
CREATE INDEX trainings_team_day ON trainings (team_id, day DESC);

CREATE TABLE attendance (
    training_id bigint  NOT NULL REFERENCES trainings ON DELETE CASCADE,
    player_id   bigint  NOT NULL REFERENCES players ON DELETE CASCADE,
    present     boolean NOT NULL,
    PRIMARY KEY (training_id, player_id)
);

CREATE TABLE callups (
    id      bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    team_id bigint NOT NULL REFERENCES teams ON DELETE CASCADE,
    day     date   NOT NULL,
    rival   text   NOT NULL DEFAULT '',
    place   text   NOT NULL DEFAULT ''
);
CREATE INDEX callups_team_day ON callups (team_id, day DESC);

-- position conserva el orden en que se añaden: es el orden de la imagen.
CREATE TABLE callup_players (
    callup_id bigint  NOT NULL REFERENCES callups ON DELETE CASCADE,
    player_id bigint  NOT NULL REFERENCES players ON DELETE CASCADE,
    position  integer NOT NULL,
    PRIMARY KEY (callup_id, player_id)
);

-- Plantilla de la imagen de convocados. En Postgres y no en GCS: el free tier
-- de Cloud Storage no cubre europe-west1 y son pocos KB por equipo.
-- layout: coordenadas relativas (0..1) donde se pintan los nombres.
CREATE TABLE team_templates (
    team_id      bigint PRIMARY KEY REFERENCES teams ON DELETE CASCADE,
    image        bytea  NOT NULL,
    content_type text   NOT NULL,
    layout       jsonb  NOT NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
