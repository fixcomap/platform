-- Una convocatoria es un partido: datos para la imagen (horas, local o
-- visitante) y, una vez jugado, resultado y tarjetas. Todo opcional.
ALTER TABLE callups
    ADD COLUMN kickoff       text,     -- "HH:MM", hora local del partido
    ADD COLUMN meet_time     text,     -- "HH:MM", citación
    ADD COLUMN home          boolean,  -- NULL = sin indicar
    ADD COLUMN goals_for     integer CHECK (goals_for >= 0),
    ADD COLUMN goals_against integer CHECK (goals_against >= 0);

-- Tarjetas por convocado: solo quien estaba convocado puede verlas.
ALTER TABLE callup_players
    ADD COLUMN yellow integer NOT NULL DEFAULT 0 CHECK (yellow BETWEEN 0 AND 2),
    ADD COLUMN red    boolean NOT NULL DEFAULT false;

-- Capitán (uno por partido) y portero(s): obligatorios para generar la imagen.
ALTER TABLE callups ADD COLUMN captain_id bigint REFERENCES players ON DELETE SET NULL;
ALTER TABLE callup_players ADD COLUMN goalkeeper boolean NOT NULL DEFAULT false;
