-- Restoring the enumerated kind CHECK would reject rows written since, so any drawing whose kind is
-- outside the original seven has to go first. It is dropped rather than rewritten: there is no
-- honest mapping from a polyline to one of the seven.
DELETE FROM blindpulse.chart_drawings
 WHERE kind NOT IN ('fib_retracement', 'fib_extension', 'supply_zone', 'demand_zone', 'trendline', 'horizontal', 'note');
ALTER TABLE blindpulse.chart_drawings DROP CONSTRAINT chart_drawings_kind_shape;
ALTER TABLE blindpulse.chart_drawings ADD CONSTRAINT chart_drawings_kind_check
    CHECK (kind IN ('fib_retracement', 'fib_extension', 'supply_zone', 'demand_zone', 'trendline', 'horizontal', 'note'));

ALTER TABLE blindpulse.blinded_feeds DROP COLUMN macro_tags;
ALTER TABLE blindpulse.blinded_feeds DROP COLUMN macro_notes;
DROP TABLE blindpulse.journal_entry_revisions;
