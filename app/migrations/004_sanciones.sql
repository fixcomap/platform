-- Amistosos: no cuentan tarjetas ni cumplen sanciones.
ALTER TABLE callups ADD COLUMN friendly boolean NOT NULL DEFAULT false;
-- Partidos de sanción por roja directa; por defecto 1, el comité puede dictar más.
ALTER TABLE callup_players
    ADD COLUMN red_games integer NOT NULL DEFAULT 1 CHECK (red_games BETWEEN 1 AND 20);
