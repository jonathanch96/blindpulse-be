package session

import "github.com/gin-gonic/gin"

type Controller interface {
	RegisterRoutes(*gin.RouterGroup)
	// RegisterStreamRoutes mounts the websocket, which is deliberately outside the bearer
	// middleware: a browser cannot set an Authorization header on a WebSocket handshake, so the
	// socket authenticates with a single-use ticket instead.
	RegisterStreamRoutes(*gin.RouterGroup)
}
