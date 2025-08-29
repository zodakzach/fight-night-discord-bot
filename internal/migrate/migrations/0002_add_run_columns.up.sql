-- Add per-guild run hour (0-23)
ALTER TABLE guild_settings ADD COLUMN run_hour INTEGER;
