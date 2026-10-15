-- Dorsal único entre los jugadores activos de un equipo (las bajas lo liberan).
-- La app ya lo valida con un mensaje claro; el índice lo garantiza ante
-- carreras. Si hubiera duplicados previos no se crea, para no impedir el
-- arranque: se avisa en el log y la validación de la app sigue activa.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM players WHERE active AND number IS NOT NULL
               GROUP BY team_id, number HAVING count(*) > 1) THEN
        RAISE WARNING 'dorsales duplicados: no se crea players_team_number_active';
    ELSE
        CREATE UNIQUE INDEX players_team_number_active ON players (team_id, number)
            WHERE active AND number IS NOT NULL;
    END IF;
END $$;
