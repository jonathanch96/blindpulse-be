package user

import "github.com/gin-gonic/gin"

type Controller interface {
	Me(*gin.Context)
	UpdateMe(*gin.Context)
	Lookup(*gin.Context)
	RegisterRoutes(*gin.RouterGroup)
}
