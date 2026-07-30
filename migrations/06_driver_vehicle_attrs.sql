-- Driver vehicle attributes for driver_info in match responses.

BEGIN;

ALTER TABLE drivers ADD COLUMN IF NOT EXISTS vehicle_attrs JSONB DEFAULT '{}'::jsonb;

UPDATE drivers
SET vehicle_attrs = '{
  "vehicle_make": "Toyota",
  "vehicle_model": "Camry",
  "vehicle_color": "White",
  "vehicle_plate": "KZ 123 ABC",
  "vehicle_year": 2020
}'::jsonb
WHERE id = '22222222-2222-4222-8222-222222222222';

COMMIT;
