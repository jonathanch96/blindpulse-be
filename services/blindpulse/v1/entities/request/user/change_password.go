package userrequest

type ChangePassword struct {
	CurrentPassword string `json:"current_password" binding:"max=128"`
	NewPassword     string `json:"new_password" binding:"required,min=8,max=128"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=NewPassword"`
}
