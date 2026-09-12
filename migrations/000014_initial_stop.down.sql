ALTER TABLE blindpulse.trades DROP CONSTRAINT IF EXISTS trades_excursions_non_negative;
ALTER TABLE blindpulse.trades DROP CONSTRAINT IF EXISTS trades_initial_risk_positive;
ALTER TABLE blindpulse.trades DROP COLUMN IF EXISTS initial_stop_loss;
