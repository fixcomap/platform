-- Nuevo estado: llegó tarde. Cuenta como asistencia (present = true).
ALTER TABLE attendance DROP CONSTRAINT attendance_status_check;
ALTER TABLE attendance ADD CONSTRAINT attendance_status_check
    CHECK (status IN ('presente', 'tarde', 'lesionado', 'justificado', 'injustificado'));
