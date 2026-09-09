package userrequest

type Update struct {
	Name      *string `json:"name" binding:"omitempty,notblank,max=120"`
	AvatarURL *string `json:"avatar_url" binding:"omitempty,url,max=2048"`
}
