package api

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/txsvc/cloudlib/helpers"
	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"

	"github.com/txsvc/apikit/auth"
	"github.com/txsvc/apikit/config"
)

const (
	// auth routes
	InitRoute   = "/auth"
	LoginRoute  = "/auth/:sig/:token"
	LogoutRoute = "/auth/:sig"

	LoginExpiresAfter = 15
)

func WithAuthEndpoints(e *echo.Echo) *echo.Echo {
	// grouped under /a/v1
	apiGroup := e.Group(NamespacePrefix)

	// add the routes
	apiGroup.POST(InitRoute, InitEndpoint)
	apiGroup.GET(LoginRoute, LoginEndpoint)
	apiGroup.DELETE(LogoutRoute, LogoutEndpoint)

	// done
	return e
}

func (c *Client) InitCommand(ds *settings.DialSettings) error {
	_, err := c.POST(fmt.Sprintf("%s%s", NamespacePrefix, InitRoute), ds, nil)
	return err
}

func InitEndpoint(c echo.Context) error {
	// get the payload
	ds := new(settings.DialSettings)
	if err := c.Bind(ds); err != nil {
		return StandardResponse(c, http.StatusBadRequest, nil)
	}

	// pre-validate the request
	if ds.Credentials == nil || ds.Credentials.ProjectID == "" {
		return StandardResponse(c, http.StatusBadRequest, nil)
	}

	// create a brand new instance so that the client can't sneak anything in we don't want
	cfg := settings.DialSettings{
		Credentials:   ds.Credentials.Clone(),
		DefaultScopes: config.GetConfig().Settings().GetScopes(),
	}

	// prepare the settings for registration
	cfg.Credentials.Token = CreateSimpleToken() // ignore anything that was provided
	cfg.Credentials.Expires = stdlib.IncT(stdlib.Now(), LoginExpiresAfter)
	cfg.Credentials.Status = StateInit // signals init

	if err := auth.UpdateStore(&cfg); err != nil {
		return StandardResponse(c, http.StatusBadRequest, nil) // FIXME: or 409/Conflict ?
	}

	// all good so far, send the confirmation
	err := helpers.MailgunSimpleEmail("ops@txs.vc", cfg.Credentials.ClientID, fmt.Sprintf("your api access credentials (%d)", stdlib.Now()), fmt.Sprintf("the token: %s\n", cfg.Credentials.Token))
	if err != nil {
		return StandardResponse(c, http.StatusBadRequest, nil)
	}
	// FIXME: the email sending has to be better !

	return StandardResponse(c, http.StatusCreated, nil)
}

func (c *Client) LoginCommand(token string) (*StatusObject, error) {
	var so StatusObject

	status, err := c.GET(fmt.Sprintf("%s%s/%s/%s", NamespacePrefix, InitRoute, signature(c.ds.Credentials.ClientID, token), token), &so)
	if status != http.StatusOK || err != nil {
		return nil, err
	}
	return &so, nil
}

func LoginEndpoint(c echo.Context) error {
	sig := c.Param("sig")
	if sig == "" {
		return ErrorResponse(c, http.StatusBadRequest, ErrInvalidRoute, "sig")
	}
	token := c.Param("token")
	if token == "" {
		return ErrorResponse(c, http.StatusBadRequest, ErrInvalidRoute, "token")
	}

	// verify the request
	ds, err := auth.LookupByToken(token)
	if ds == nil && err != nil {
		return ErrorResponse(c, http.StatusBadRequest, ErrInternalError, "token")
	}
	if ds == nil && err == nil {
		return ErrorResponse(c, http.StatusBadRequest, config.ErrInitializingConfiguration, "not found") // simply not there ...
	}

	// compare provided signature with the expected signature
	if sig != signature(ds.Credentials.ClientID, ds.Credentials.Token) {
		return ErrorResponse(c, http.StatusBadRequest, config.ErrInitializingConfiguration, "invalid sig")
	}

	// check if the token is still valid
	if ds.Credentials.Expires < stdlib.Now() {
		return ErrorResponse(c, http.StatusBadRequest, auth.ErrTokenExpired, "expired")
	}

	// everything checks out, create/register the real credentials now ...
	cfg := ds.Clone()           // clone, otherwise stupid things happen with pointers !
	cfg.Credentials.Expires = 0 // FIXME: really never ?
	cfg.Credentials.Token = CreateSimpleToken()
	cfg.Credentials.Status = StateAuthorized

	// FIXME: what about scopes ?

	if err := auth.UpdateStore(&cfg); err != nil {
		fmt.Println(err)
		return ErrorResponse(c, http.StatusBadRequest, config.ErrInitializingConfiguration, "can't register")
	}

	// just send the token back
	resp := StatusObject{
		Status:  http.StatusOK,
		Message: cfg.Credentials.Token,
	}

	return StandardResponse(c, http.StatusOK, resp)
}

func (c *Client) LogoutCommand() error {
	_, err := c.DELETE(fmt.Sprintf("%s%s/%s", NamespacePrefix, InitRoute, signature(c.ds.Credentials.ClientID, c.ds.Credentials.Token)), nil, nil)
	if err != nil {
		return err
	}

	return nil
}

func LogoutEndpoint(c echo.Context) error {
	sig := c.Param("sig")
	if sig == "" {
		return ErrorResponse(c, http.StatusBadRequest, ErrInvalidRoute, "sig")
	}
	token, err := auth.GetBearerToken(c.Request())
	if err != nil {
		return ErrorResponse(c, http.StatusUnauthorized, err, "")
	}

	// verify the request
	cfg, err := auth.LookupByToken(token)
	if cfg == nil && err != nil {
		return ErrorResponse(c, http.StatusBadRequest, ErrInternalError, "token")
	}

	// compare provided signature with the expected signature
	if sig != signature(cfg.Credentials.ClientID, cfg.Credentials.Token) {
		return ErrorResponse(c, http.StatusBadRequest, config.ErrInitializingConfiguration, "invalid sig")
	}

	// update the cache and store
	cfg.Credentials.Status = StateUndefined // just set to invalid and expired
	cfg.Credentials.Expires = stdlib.Now() - 1
	if err := auth.UpdateStore(cfg); err != nil {
		return ErrorResponse(c, http.StatusBadRequest, err, "update store")
	}

	return StandardResponse(c, http.StatusOK, nil)
}

// signature returns a MD5(clientid+token) as this is only known locally ...
func signature(clientid, token string) string {
	return stdlib.Fingerprint(fmt.Sprintf("%s%s", clientid, token))
}

func CreateSimpleToken() string {
	token, _ := stdlib.UUID()
	return token
}
