-- Pizarras tácticas con nombre: fichas, balón y flechas en coordenadas del
-- campo (JSON validado por la app). Independientes de los partidos.
CREATE TABLE boards (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    team_id    bigint NOT NULL REFERENCES teams ON DELETE CASCADE,
    name       text   NOT NULL,
    state      jsonb  NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX boards_team ON boards (team_id, updated_at DESC);
