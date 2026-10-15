-- Posiciones en las que juega (códigos separados por comas, p. ej. "DFC,LD"):
-- las sugiere el planteamiento al colocar a cada jugador.
ALTER TABLE players ADD COLUMN positions text NOT NULL DEFAULT '';

-- Cualidades estilo FIFA, del 1 al 10 en pasos de 0,5. Privadas del cuerpo
-- técnico (solo admin). NULL = sin valorar.
CREATE TABLE player_skills (
    player_id bigint PRIMARY KEY REFERENCES players ON DELETE CASCADE,
    pace      numeric(3,1),
    shooting  numeric(3,1),
    passing   numeric(3,1),
    dribbling numeric(3,1),
    defending numeric(3,1),
    physical  numeric(3,1),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT skills_range CHECK (
        (pace      IS NULL OR (pace      BETWEEN 1 AND 10 AND pace * 2      = trunc(pace * 2))) AND
        (shooting  IS NULL OR (shooting  BETWEEN 1 AND 10 AND shooting * 2  = trunc(shooting * 2))) AND
        (passing   IS NULL OR (passing   BETWEEN 1 AND 10 AND passing * 2   = trunc(passing * 2))) AND
        (dribbling IS NULL OR (dribbling BETWEEN 1 AND 10 AND dribbling * 2 = trunc(dribbling * 2))) AND
        (defending IS NULL OR (defending BETWEEN 1 AND 10 AND defending * 2 = trunc(defending * 2))) AND
        (physical  IS NULL OR (physical  BETWEEN 1 AND 10 AND physical * 2  = trunc(physical * 2))))
);
