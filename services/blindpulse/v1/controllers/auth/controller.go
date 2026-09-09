package auth

import (
	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/response"
	appvalidator "github.com/jblabs/blindpulse-be/pkg/validator"
	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	authreq "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/auth"
	authresp "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/auth"
)

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	auth := group.Group("/auth")
	auth.POST("/register", c.Register)
	auth.POST("/login", c.Login)
	auth.POST("/google", c.Google)
	auth.POST("/refresh", c.Refresh)
	auth.POST("/logout", c.Logout)
}

// Register godoc
// @Summary Register a new account
// @Tags auth
// @Accept json
// @Produce json
// @Param body body authrequest.Register true "Registration payload"
// @Success 201 {object} response.Envelope{data=authresponse.Session}
// @Failure 400 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /auth/register [post]
func (c *controller) Register(ctx *gin.Context) {
	var request authreq.Register
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	session, err := c.users.Register(ctx.Request.Context(), userdomain.RegisterInput{
		Email: request.Email, Name: request.Name, Password: request.Password,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "USER_REGISTERED", authresp.FromSession(session))
}

// Login godoc
// @Summary Log in
// @Tags auth
// @Accept json
// @Produce json
// @Param body body authrequest.Login true "Login payload"
// @Success 200 {object} response.Envelope{data=authresponse.Session}
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /auth/login [post]
func (c *controller) Login(ctx *gin.Context) {
	var request authreq.Login
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	session, err := c.users.Authenticate(ctx.Request.Context(), request.Email, request.Password)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "LOGIN_SUCCESS", authresp.FromSession(session))
}

// Google godoc
// @Summary Sign in with a verified Google ID token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body authrequest.Google true "Google ID token"
// @Success 200 {object} response.Envelope{data=authresponse.Session}
// @Failure 400 {object} response.Envelope
// @Failure 401 {object} response.Envelope
// @Router /auth/google [post]
func (c *controller) Google(ctx *gin.Context) {
	var request authreq.Google
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	session, err := c.users.AuthenticateGoogle(ctx.Request.Context(), request.IDToken)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "LOGIN_SUCCESS", authresp.FromSession(session))
}

// Refresh godoc
// @Summary Rotate a refresh token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body authrequest.Refresh true "Refresh payload"
// @Success 200 {object} response.Envelope{data=authresponse.Session}
// @Failure 401 {object} response.Envelope
// @Router /auth/refresh [post]
func (c *controller) Refresh(ctx *gin.Context) {
	var request authreq.Refresh
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	session, err := c.users.Refresh(ctx.Request.Context(), request.RefreshToken)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "TOKEN_REFRESHED", authresp.FromSession(session))
}

// Logout godoc
// @Summary Revoke a refresh token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body authrequest.Refresh true "Refresh token"
// @Success 200 {object} response.Envelope
// @Failure 400 {object} response.Envelope
// @Router /auth/logout [post]
func (c *controller) Logout(ctx *gin.Context) {
	var request authreq.Refresh
	if err := ctx.ShouldBindJSON(&request); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", appvalidator.Translate(err)))
		return
	}
	if err := c.users.Logout(ctx.Request.Context(), request.RefreshToken); err != nil {
		response.Error(ctx, err)
		return
	}
	response.NoData(ctx, "LOGOUT_SUCCESS")
}
