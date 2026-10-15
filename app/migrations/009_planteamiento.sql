-- Planteamiento del partido: formación y quién ocupa cada puesto. Queda
-- registrado con la convocatoria; no sale en la imagen.
ALTER TABLE callups ADD COLUMN formation text;

-- El FK a callup_players garantiza que solo se coloca a convocados y que, al
-- quitar a alguien de la convocatoria, desaparece también del planteamiento.
CREATE TABLE callup_lineup (
    callup_id bigint  NOT NULL,
    slot      integer NOT NULL CHECK (slot BETWEEN 0 AND 10),
    player_id bigint  NOT NULL,
    PRIMARY KEY (callup_id, slot),
    UNIQUE (callup_id, player_id),
    FOREIGN KEY (callup_id, player_id) REFERENCES callup_players (callup_id, player_id) ON DELETE CASCADE
);
