package accountrequest

type Open struct {
	Name            string   `json:"name" binding:"required,min=1,max=120"`
	StrategyProfile string   `json:"strategy_profile" binding:"omitempty,max=160"`
	Currency        string   `json:"currency" binding:"omitempty,len=3,alpha"`
	InitialBalance  string   `json:"initial_balance" binding:"required,numeric"`
	Risk            RiskRule `json:"risk"`
}

// RiskRule mirrors the bracket dock's gates. Every field is a string so a decimal reaches the
// domain exactly as typed - binding a price or a percentage through float64 silently rounds it.
type RiskRule struct {
	RiskPerTradePct     string `json:"risk_per_trade_pct" binding:"omitempty,numeric"`
	MaxDailyDrawdownPct string `json:"max_daily_drawdown_pct" binding:"omitempty,numeric"`
	MinRiskReward       string `json:"min_risk_reward" binding:"omitempty,numeric"`
	MaxOpenPositions    *int   `json:"max_open_positions" binding:"omitempty,min=1,max=100"`
	Leverage            string `json:"leverage" binding:"omitempty,numeric"`
}

type Reset struct {
	Reason          string   `json:"reason" binding:"required,min=1,max=240"`
	Name            string   `json:"name" binding:"omitempty,max=120"`
	StrategyProfile string   `json:"strategy_profile" binding:"omitempty,max=160"`
	InitialBalance  string   `json:"initial_balance" binding:"omitempty,numeric"`
	Risk            RiskRule `json:"risk"`
}
