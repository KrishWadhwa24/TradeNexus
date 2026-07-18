-- No-op: re-adding the stale per-day UNIQUE would reintroduce the bug (and could
-- fail against existing multi-signal delivery rows). The per-signal unique index
-- from 0008 remains the source of truth.
SELECT 1;
