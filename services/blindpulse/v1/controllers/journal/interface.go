package journal

import "github.com/gin-gonic/gin"

type Controller interface {
	RegisterRoutes(*gin.RouterGroup)
}
