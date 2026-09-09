package account

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
	accountrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/account"
	accountresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/account"
	"github.com/shopspring/decimal"
)

func NewController(accounts accountdomain.Service) Controller {
	return &controller{accounts: accounts}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/accounts", c.open)
	group.GET("/accounts", c.listTrees)
	group.GET("/accounts/:id", c.get)
	group.POST("/accounts/:id/reset", c.reset)
	group.GET("/accounts/:id/tree", c.tree)
	group.GET("/accounts/:id/ledger", c.ledger)
	group.GET("/accounts/:id/ledger/verify", c.verify)
}

func bind(ctx *gin.Context, value any) bool {
	if err := ctx.ShouldBindJSON(value); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "request", Rule: "invalid", Message: err.Error()}}))
		return false
	}
	return true
}

func actor(ctx *gin.Context) identity.Identity {
	return identity.MustFromContext(ctx.Request.Context())
}

func accountID(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		response.Error(ctx, apperror.New("ACCOUNT_NOT_FOUND"))
		return uuid.Nil, false
	}
	return id, true
}

// open godoc
// @Summary Open a trading account
// @Description Starts a new reset tree at iteration 1 with an opening ledger entry.
// @Tags accounts
// @Security BearerAuth
// @Param body body accountrequest.Open true "Account"
// @Success 201 {object} response.Envelope{data=accountresponse.Account}
// @Failure 400 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /accounts [post]
func (c *controller) open(ctx *gin.Context) {
	var request accountrequest.Open
	if !bind(ctx, &request) {
		return
	}
	balance, err := decimal.NewFromString(request.InitialBalance)
	if err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "initial_balance", Rule: "numeric", Message: "initial_balance must be a decimal string"}}))
		return
	}
	risk, fields := parseRisk(request.Risk)
	if len(fields) > 0 {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", fields))
		return
	}
	entity, err := c.accounts.Open(ctx, actor(ctx).UserID, accountdomain.OpenInput{
		Name: request.Name, StrategyProfile: request.StrategyProfile, Currency: request.Currency,
		InitialBalance: balance, Risk: risk,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "ACCOUNT_OPENED", accountresponse.FromDomain(*entity))
}

// listTrees godoc
// @Summary List account trees
// @Description Returns every reset tree the caller owns, each with all of its iterations.
// @Tags accounts
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=[]accountresponse.Tree}
// @Router /accounts [get]
func (c *controller) listTrees(ctx *gin.Context) {
	trees, err := c.accounts.ListTrees(ctx, actor(ctx).UserID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ACCOUNTS_LISTED", accountresponse.FromTrees(trees))
}

// get godoc
// @Summary Get one account iteration
// @Tags accounts
// @Security BearerAuth
// @Param id path string true "Account ID"
// @Success 200 {object} response.Envelope{data=accountresponse.Account}
// @Failure 404 {object} response.Envelope
// @Router /accounts/{id} [get]
func (c *controller) get(ctx *gin.Context) {
	id, ok := accountID(ctx)
	if !ok {
		return
	}
	entity, err := c.accounts.Get(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ACCOUNT_FETCHED", accountresponse.FromDomain(*entity))
}

// reset godoc
// @Summary Reset an account into a new iteration
// @Description Seals the active iteration and opens the next one. Nothing is deleted or edited.
// @Tags accounts
// @Security BearerAuth
// @Param id path string true "Account ID"
// @Param body body accountrequest.Reset true "Reset"
// @Success 201 {object} response.Envelope{data=accountresponse.Account}
// @Failure 403 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /accounts/{id}/reset [post]
func (c *controller) reset(ctx *gin.Context) {
	id, ok := accountID(ctx)
	if !ok {
		return
	}
	var request accountrequest.Reset
	if !bind(ctx, &request) {
		return
	}
	input := accountdomain.ResetInput{Reason: request.Reason, Name: request.Name, StrategyProfile: request.StrategyProfile}
	if request.InitialBalance != "" {
		balance, err := decimal.NewFromString(request.InitialBalance)
		if err != nil {
			response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{{Field: "initial_balance", Rule: "numeric", Message: "initial_balance must be a decimal string"}}))
			return
		}
		input.InitialBalance = &balance
	}
	risk, fields := parseRisk(request.Risk)
	if len(fields) > 0 {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", fields))
		return
	}
	input.Risk = risk
	entity, err := c.accounts.Reset(ctx, actor(ctx).UserID, id, input)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "ACCOUNT_RESET", accountresponse.FromDomain(*entity))
}

// tree godoc
// @Summary Get one account's full reset tree
// @Tags accounts
// @Security BearerAuth
// @Param id path string true "Any account ID within the tree"
// @Success 200 {object} response.Envelope{data=accountresponse.Tree}
// @Failure 404 {object} response.Envelope
// @Router /accounts/{id}/tree [get]
func (c *controller) tree(ctx *gin.Context) {
	id, ok := accountID(ctx)
	if !ok {
		return
	}
	entity, err := c.accounts.Get(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	tree, err := c.accounts.GetTree(ctx, actor(ctx).UserID, entity.RootAccountID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ACCOUNT_TREE_FETCHED", accountresponse.FromTree(*tree))
}

// ledger godoc
// @Summary Read an account's immutable ledger
// @Tags accounts
// @Security BearerAuth
// @Param id path string true "Account ID"
// @Success 200 {object} response.Envelope{data=[]accountresponse.LedgerEntry}
// @Failure 404 {object} response.Envelope
// @Router /accounts/{id}/ledger [get]
func (c *controller) ledger(ctx *gin.Context) {
	id, ok := accountID(ctx)
	if !ok {
		return
	}
	entries, err := c.accounts.Ledger(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ACCOUNT_LEDGER_FETCHED", accountresponse.FromLedgerEntries(entries))
}

// verify godoc
// @Summary Verify an account ledger's hash chain
// @Description Recomputes every link and compares the result against the sealed root hash.
// @Tags accounts
// @Security BearerAuth
// @Param id path string true "Account ID"
// @Success 200 {object} response.Envelope{data=accountresponse.Verification}
// @Failure 404 {object} response.Envelope
// @Router /accounts/{id}/ledger/verify [get]
func (c *controller) verify(ctx *gin.Context) {
	id, ok := accountID(ctx)
	if !ok {
		return
	}
	result, err := c.accounts.VerifyLedger(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ACCOUNT_LEDGER_VERIFIED", accountresponse.Verification{
		AccountID: result.AccountID, Entries: result.Entries, RootHash: result.RootHash,
		StoredRootHash: result.StoredRootHash, Valid: result.Valid, BrokenAtSequence: result.BrokenAtSequence,
	})
}

// parseRisk turns the request's decimal strings into domain values, collecting every bad field
// rather than rejecting on the first one - a form with three wrong numbers should say so once.
func parseRisk(request accountrequest.RiskRule) (accountdomain.RiskInput, []apperror.FieldError) {
	fields := make([]apperror.FieldError, 0)
	input := accountdomain.RiskInput{MaxOpenPositions: request.MaxOpenPositions}
	parse := func(name, raw string, target **decimal.Decimal) {
		if raw == "" {
			return
		}
		value, err := decimal.NewFromString(raw)
		if err != nil {
			fields = append(fields, apperror.FieldError{Field: name, Rule: "numeric", Message: name + " must be a decimal string"})
			return
		}
		*target = &value
	}
	parse("risk_per_trade_pct", request.RiskPerTradePct, &input.RiskPerTradePct)
	parse("max_daily_drawdown_pct", request.MaxDailyDrawdownPct, &input.MaxDailyDrawdownPct)
	parse("min_risk_reward", request.MinRiskReward, &input.MinRiskReward)
	parse("leverage", request.Leverage, &input.Leverage)
	return input, fields
}
