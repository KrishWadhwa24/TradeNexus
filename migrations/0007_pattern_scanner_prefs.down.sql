DELETE FROM user_scanner_prefs
WHERE scanner_key IN (
    'pattern_downtrend_breakout',
    'pattern_rectangle',
    'pattern_cup_handle'
);
