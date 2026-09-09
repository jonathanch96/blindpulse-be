package user

import (
	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	appvalidator "github.com/jblabs/blindpulse-be/pkg/validator"
	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	userreq "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/user"
	userresp "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/user"
)

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	users := group.Group("/users")
	users.GET("/me", c.Me)
	users.PATCH("/me", c.UpdateMe)
	users.PATCH("/me/password", c.ChangePassword)
	users.GET("/lookup", c.Lookup)
}

// Me godoc
// @Summary Get current profile
// @Tags users
// @Security BearerAuth
// @Produce json
// @Success 200 {object} response.Envelope{data=userresponse.User}
// @Failure 401 {object} response.Envelope
// @Router /users/me [get]
func (c *controller) Me(ctx *gin.Context) {
	current := identity.MustFromContext(ctx.Request.Context())
	entity, err := c.users.GetByID(ctx.Request.Context(), current.UserID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "USER_FETCHED", userresp.FromDomain(*entity))
}

// UpdateMe godoc
// @Summary Update current profile
// @Tags users
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body userrequest.Update true "Profile changes"
// @Success 200 {object} response.Envelope{data=userresponse.User}
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /users/me [patch]
func (c *controller) UpdateMe(ctx *gin.Context) {
	var request userreq.Update
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	current := identity.MustFromContext(ctx.Request.Context())
	entity, err := c.users.UpdateProfile(ctx.Request.Context(), current.UserID, userdomain.UpdateProfileInput{
		Name: request.Name, AvatarURL: request.AvatarURL,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "USER_UPDATED", userresp.FromDomain(*entity))
}

// ChangePassword godoc
// @Summary Change the current user's password
// @Tags users
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body userrequest.ChangePassword true "Password change"
// @Success 200 {object} response.Envelope
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /users/me/password [patch]
func (c *controller) ChangePassword(ctx *gin.Context) {
	var request userreq.ChangePassword
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	current := identity.MustFromContext(ctx.Request.Context())
	if err := c.users.ChangePassword(ctx.Request.Context(), current.UserID, userdomain.ChangePasswordInput{
		CurrentPassword: request.CurrentPassword, NewPassword: request.NewPassword,
	}); err != nil {
		response.Error(ctx, err)
		return
	}
	response.NoData(ctx, "PASSWORD_CHANGED")
}

// Lookup godoc
// @Summary Find a user by exact email
// @Tags users
// @Security BearerAuth
// @Produce json
// @Param email query string true "Exact email address"
// @Success 200 {object} response.Envelope{data=userresponse.Lookup}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /users/lookup [get]
func (c *controller) Lookup(ctx *gin.Context) {
	var request userreq.Lookup
	if err := ctx.ShouldBindQuery(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	entity, err := c.users.FindByEmail(ctx.Request.Context(), request.Email)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "USER_FETCHED", userresp.LookupFromDomain(*entity))
}
