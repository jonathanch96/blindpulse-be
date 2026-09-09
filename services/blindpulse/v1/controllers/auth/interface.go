package auth

import "github.com/gin-gonic/gin"

type Controller interface {
	Register(*gin.Context)
	Login(*gin.Context)
	Refresh(*gin.Context)
	Logout(*gin.Context)
	RegisterRoutes(*gin.RouterGroup)
}
