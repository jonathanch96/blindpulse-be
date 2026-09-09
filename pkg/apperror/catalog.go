package apperror

import "net/http"

type definition struct {
	HTTP    int
	Message string
}

var catalog = map[string]definition{
	// Request shape
	"VALIDATION_FAILED":      {http.StatusBadRequest, "Request validation failed"},
	"INVALID_TIMEFRAME":      {http.StatusBadRequest, "Timeframe is not supported"},
	"INVALID_PLAYBACK_SPEED": {http.StatusBadRequest, "Playback speed is outside the supported range"},
	"INVALID_CURSOR":         {http.StatusBadRequest, "Replay cursor is outside the session window"},
	"ORDER_STOP_REQUIRED":    {http.StatusBadRequest, "Every entry must carry a hard stop loss"},
	"ORDER_STOP_INVALID":     {http.StatusBadRequest, "Stop loss is on the wrong side of the entry"},
	"ORDER_TARGET_INVALID":   {http.StatusBadRequest, "Take profit is on the wrong side of the entry"},
	"ORDER_QUANTITY_INVALID": {http.StatusBadRequest, "Order quantity must be greater than zero"},

	// Authentication
	"UNAUTHENTICATED":          {http.StatusUnauthorized, "Authentication is required"},
	"INVALID_CREDENTIALS":      {http.StatusUnauthorized, "Email or password is incorrect"},
	"INVALID_CURRENT_PASSWORD": {http.StatusUnauthorized, "Current password is incorrect"},
	"GOOGLE_TOKEN_INVALID":     {http.StatusUnauthorized, "Google sign-in token is invalid"},
	"SSO_ASSERTION_INVALID":    {http.StatusUnauthorized, "The SSO assertion could not be verified"},
	"REFRESH_TOKEN_INVALID":    {http.StatusUnauthorized, "Refresh token is invalid or has been rotated"},

	// Authorization
	"FORBIDDEN":          {http.StatusForbidden, "You are not permitted to perform this action"},
	"NOT_ACCOUNT_OWNER":  {http.StatusForbidden, "You do not own this trading account"},
	"NOT_SESSION_OWNER":  {http.StatusForbidden, "You do not own this replay session"},
	"SESSION_CLOSED":     {http.StatusForbidden, "The replay session has already been closed"},
	"ACCOUNT_ARCHIVED":   {http.StatusForbidden, "This account iteration is archived and immutable"},
	"LEDGER_IMMUTABLE":   {http.StatusForbidden, "Sealed ledger entries cannot be modified"},
	"REVEAL_LOCKED":      {http.StatusForbidden, "The mystery reveal unlocks after the session is closed"},
	"MEDIA_LINK_INVALID": {http.StatusForbidden, "This media link is invalid or has expired"},

	// Absence
	"USER_NOT_FOUND":       {http.StatusNotFound, "User not found"},
	"ACCOUNT_NOT_FOUND":    {http.StatusNotFound, "Trading account not found"},
	"SESSION_NOT_FOUND":    {http.StatusNotFound, "Replay session not found"},
	"FEED_NOT_FOUND":       {http.StatusNotFound, "Blinded feed not found"},
	"INSTRUMENT_NOT_FOUND": {http.StatusNotFound, "Instrument not found"},
	"ORDER_NOT_FOUND":      {http.StatusNotFound, "Order not found"},
	"TRADE_NOT_FOUND":      {http.StatusNotFound, "Trade not found"},
	"JOURNAL_NOT_FOUND":    {http.StatusNotFound, "Journal entry not found"},
	"DRAWING_NOT_FOUND":    {http.StatusNotFound, "Chart drawing not found"},
	"BARS_EXHAUSTED":       {http.StatusNotFound, "The feed has no further bars in this window"},

	// Conflict
	"EMAIL_ALREADY_REGISTERED": {http.StatusConflict, "Email is already registered"},
	"ACCOUNT_NAME_TAKEN":       {http.StatusConflict, "An account with this name already exists"},
	"SESSION_ALREADY_OPEN":     {http.StatusConflict, "This account already has an open replay session"},
	"CONCURRENT_MODIFICATION":  {http.StatusConflict, "The record was modified by another request"},
	"DUPLICATE_ORDER":          {http.StatusConflict, "An order with this idempotency key already exists"},
	"ALREADY_REVEALED":         {http.StatusConflict, "This session has already been unblinded"},

	// Uploads
	"FILE_TOO_LARGE":         {http.StatusRequestEntityTooLarge, "The uploaded file is too large"},
	"UNSUPPORTED_MEDIA_TYPE": {http.StatusUnsupportedMediaType, "The uploaded file type is not supported"},

	// Risk gates - the simulator refuses the order rather than silently clamping it, so the
	// discipline index records a rejected attempt instead of an invented, softer trade.
	"RISK_STOP_TOO_WIDE":      {http.StatusUnprocessableEntity, "Stop loss exceeds the account risk-per-trade limit"},
	"RISK_REWARD_TOO_LOW":     {http.StatusUnprocessableEntity, "Risk-to-reward is below the account minimum"},
	"DAILY_DRAWDOWN_BREACHED": {http.StatusUnprocessableEntity, "The daily drawdown gate is breached; trading is halted"},
	"INSUFFICIENT_MARGIN":     {http.StatusUnprocessableEntity, "Account equity cannot support this position"},
	"MAX_POSITIONS_REACHED":   {http.StatusUnprocessableEntity, "The account has reached its open-position limit"},
	"POSITION_ALREADY_CLOSED": {http.StatusUnprocessableEntity, "The position is already closed"},
	"FEED_WINDOW_TOO_SHORT":   {http.StatusUnprocessableEntity, "The requested window has too few bars to replay"},

	"RATE_LIMITED": {http.StatusTooManyRequests, "Too many requests"},

	"INTERNAL_ERROR":       {http.StatusInternalServerError, "An unexpected error occurred"},
	"EVENT_PUBLISH_FAILED": {http.StatusBadGateway, "The event bus is unavailable"},
	"CACHE_UNAVAILABLE":    {http.StatusServiceUnavailable, "The replay cache is unavailable"},
}

func Definition(code string) (int, string, bool) {
	d, ok := catalog[code]
	return d.HTTP, d.Message, ok
}

func Catalog() map[string]struct {
	HTTP    int
	Message string
} {
	result := make(map[string]struct {
		HTTP    int
		Message string
	}, len(catalog))
	for code, d := range catalog {
		result[code] = struct {
			HTTP    int
			Message string
		}{d.HTTP, d.Message}
	}
	return result
}
