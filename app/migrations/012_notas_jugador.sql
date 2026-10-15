-- Notas privadas del cuerpo técnico sobre cada jugador (solo admin).
CREATE TABLE player_notes (
    player_id    bigint PRIMARY KEY REFERENCES players ON DELETE CASCADE,
    observations text NOT NULL DEFAULT '',
    improve      text NOT NULL DEFAULT '',
    updated_at   timestamptz NOT NULL DEFAULT now()
);
