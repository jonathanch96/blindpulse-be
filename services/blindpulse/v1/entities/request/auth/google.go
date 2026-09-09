package authrequest

type Google struct {
	IDToken string `json:"id_token" binding:"required"`
}
