-- R-multiple is measured against the risk the position was *sized* on, and the trade had no record of
-- it. Risk() read the current stop, so the moment a trader moved one — breakeven being a first-class
-- action — 1R changed retroactively, and after a move to entry the distance was zero and every
-- R-multiple came out 0.0000. A trade that lost real money reporting 0R reads as a scratch, which is
-- the opposite of what happened.
--
-- Nullable, and deliberately so. Every row written from here on has it, because the fill sets it. For
-- rows that predate the column it is recoverable only while the stop has not moved: where stop_loss
-- still differs from entry_price that is the original stop and the backfill is exact, and where it
-- does not, the stop was already at breakeven and the original distance is simply gone. NULL says
-- that, where a backfilled entry_price would assert a risk of zero that was never true.
ALTER TABLE blindpulse.trades
    ADD COLUMN initial_stop_loss NUMERIC(20, 10);

UPDATE blindpulse.trades
   SET initial_stop_loss = stop_loss
 WHERE initial_stop_loss IS NULL
   AND stop_loss <> entry_price;

-- When it is present it must be a real distance: a position sized from dollar risk over a stop
-- distance needs that distance to be positive, and the order gate already refuses a stop at the entry
-- price. So an R-multiple computed from a non-NULL value can never divide by zero.
ALTER TABLE blindpulse.trades
    ADD CONSTRAINT trades_initial_risk_positive
        CHECK (initial_stop_loss IS NULL OR initial_stop_loss <> entry_price);

-- Excursions are price distances and non-negative: "how far did this go against me before it
-- resolved" is only meaningful next to the stop distance, and they were being stored as signed money
-- figures, so an adverse excursion came out negative and in the wrong units entirely.
--
-- The pre-existing rows are rewritten to NULL rather than converted: the stored number is a money
-- amount whose conversion needs the size at the time, and "not measured" is honest where a rescaled
-- guess would not be.
UPDATE blindpulse.trades
   SET max_adverse_excursion = NULL, max_favorable_excursion = NULL
 WHERE max_adverse_excursion < 0 OR max_favorable_excursion < 0
    OR max_adverse_excursion IS NOT NULL OR max_favorable_excursion IS NOT NULL;

ALTER TABLE blindpulse.trades
    ADD CONSTRAINT trades_excursions_non_negative
        CHECK (
            (max_adverse_excursion IS NULL OR max_adverse_excursion >= 0)
            AND (max_favorable_excursion IS NULL OR max_favorable_excursion >= 0)
        );
