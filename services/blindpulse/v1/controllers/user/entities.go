package user

import userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"

type controller struct{ users userdomain.Service }

func NewController(users userdomain.Service) Controller { return &controller{users: users} }
