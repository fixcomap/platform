-- Lista de posiciones reducida a la que usa el cuerpo técnico. Se convierten
-- las ya cargadas manteniendo la principal primero y sin repetir.
UPDATE players p SET positions = coalesce((
    SELECT string_agg(code, ',' ORDER BY ord)
    FROM (
        SELECT CASE c
                   WHEN 'CAD' THEN 'LD'  WHEN 'CAI' THEN 'LI'
                   WHEN 'MCD' THEN 'MC'  WHEN 'MCO' THEN 'ENG'
                   WHEN 'MD'  THEN 'VD'  WHEN 'ED'  THEN 'VD'
                   WHEN 'MI'  THEN 'VI'  WHEN 'EI'  THEN 'VI'
                   ELSE c END AS code,
               min(ord) AS ord
        FROM unnest(string_to_array(p.positions, ',')) WITH ORDINALITY AS u(c, ord)
        GROUP BY 1
    ) mapped), '')
WHERE positions <> '';
