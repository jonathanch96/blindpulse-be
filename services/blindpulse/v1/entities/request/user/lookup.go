package userrequest

type Lookup struct {
	Email string `form:"email" binding:"required,email,max=255"`
}
