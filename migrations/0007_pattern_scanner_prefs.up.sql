-- Enable pattern scanner alert preferences for existing users.

INSERT INTO user_scanner_prefs (user_id, scanner_key, enabled)
SELECT u.id, k.scanner_key, TRUE
FROM users u
CROSS JOIN (
    VALUES
        ('pattern_downtrend_breakout'),
        ('pattern_rectangle'),
        ('pattern_cup_handle')
) AS k(scanner_key)
ON CONFLICT (user_id, scanner_key) DO NOTHING;
