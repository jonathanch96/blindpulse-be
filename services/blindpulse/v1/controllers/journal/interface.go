package journal

import "github.com/gin-gonic/gin"

type Controller interface {
	RegisterRoutes(*gin.RouterGroup)
	// RegisterMediaRoutes mounts the image bytes outside the bearer middleware, because an <img src>
	// cannot carry an Authorization header. The signed link is the authority instead.
	RegisterMediaRoutes(*gin.RouterGroup)
}
