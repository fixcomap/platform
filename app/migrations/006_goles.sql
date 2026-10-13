-- Goles por convocado, para el conteo de goleadores.
ALTER TABLE callup_players
    ADD COLUMN goals integer NOT NULL DEFAULT 0 CHECK (goals BETWEEN 0 AND 30);

-- Observaciones libres del partido.
ALTER TABLE callups ADD COLUMN notes text NOT NULL DEFAULT '';
