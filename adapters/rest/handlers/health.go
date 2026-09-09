package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	"github.com/jblabs/blindpulse-be/pkg/response"
	"gorm.io/gorm"
)

// Health answers "is this process alive". It deliberately touches no dependency: a liveness probe
// that fails when the database is slow gets the container killed for someone else's outage.
func Health(c *gin.Context) {
	response.OK(c, "OK", gin.H{"status": "ok"})
}

// Ready answers "can this process serve traffic", so it does check the dependencies a request
// needs. A configured-but-unreachable Redis fails readiness: the replay hot path would otherwise
// silently degrade to per-instance state behind a load balancer that assumes it is shared.
func Ready(db *gorm.DB, redis *cache.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := config.Ping(c.Request.Context(), db); err != nil {
			response.Error(c, apperror.Wrap(err, "INTERNAL_ERROR"))
			return
		}
		if err := redis.Ping(c.Request.Context()); err != nil {
			response.Error(c, apperror.Wrap(err, "CACHE_UNAVAILABLE"))
			return
		}
		response.OK(c, "OK", gin.H{"status": "ready", "cache": redis.Enabled()})
	}
}
