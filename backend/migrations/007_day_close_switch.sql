-- 007_day_close_switch: the day-close feature becomes a switch.
--
-- A store that never counts its drawer has no use for "open the day" and
-- "close the day", and the no-auto-open rule that protects the cash count
-- only blocks its till. With day_close_enabled = false the first sale or
-- receipt of a business date opens the day itself (float zero, audited as
-- auto_open) and seals a day left open from an earlier date (auto_close).
-- Seeded true so an upgrade changes nothing until the owner flips it.
INSERT INTO settings (key, value) VALUES ('day_close_enabled', 'true')
ON CONFLICT (key) DO NOTHING;
