-- 008_backdate_limit: how far back an admin may date a sale becomes a
-- setting (plan 2026-10-24). It was the constant 40 in the invoice handler
-- and the till. Seeded at 40 so an upgrade changes nothing until the owner
-- moves it; 0 switches back-dating off; the rule caps it at 90, the furthest
-- the FBR sandbox has been seen to accept (audit FBR_SANDBOX_2026-10-13).
INSERT INTO settings (key, value) VALUES ('backdate_limit_days', '40')
ON CONFLICT (key) DO NOTHING;
