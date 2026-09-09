package authrequest

type Register struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Name     string `json:"name" binding:"required,notblank,max=120"`
	Password string `json:"password" binding:"required,min=8,max=128"`
}
