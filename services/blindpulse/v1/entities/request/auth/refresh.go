package authrequest

type Refresh struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}
