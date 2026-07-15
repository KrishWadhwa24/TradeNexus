-- Admin flag: gates the candle-management tools (count/delete/refetch by date).
-- The admin account itself is upserted on boot from ADMIN_EMAIL/ADMIN_PASSWORD.
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
