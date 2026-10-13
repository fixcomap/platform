-- Estado de la asistencia: además de vino/no vino, el motivo de la ausencia.
-- Expand/contract: present se mantiene (la escribe el código nuevo) para que
-- la revisión anterior siga funcionando durante el despliegue; se borrará en
-- una migración posterior.
ALTER TABLE attendance ADD COLUMN status text;
UPDATE attendance SET status = CASE WHEN present THEN 'presente' ELSE 'injustificado' END;
ALTER TABLE attendance
    ALTER COLUMN status SET NOT NULL,
    ADD CONSTRAINT attendance_status_check
        CHECK (status IN ('presente', 'lesionado', 'justificado', 'injustificado'));
