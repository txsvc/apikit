package main

import (
	"github.com/txsvc/stdlib/v2"

	"github.com/txsvc/apikit/auth"
	"github.com/txsvc/apikit/config"
	"github.com/txsvc/stdlib/v2/settings"
)

// FIXME: make this Google AppEngine specific !

func (c *appConfig) Info() *config.Info {
	return c.info
}

// ConfigLocation returns the config location that was set using SetConfigLocation().
// If no location is defined, GetConfigLocation looks for ENV['CONFIG_LOCATION'] or
// returns DefaultConfigLocation() if no environment variable was set.
func (c *appConfig) ConfigLocation() string {
	return "" // nothing since we don't access any local resources
}

func (c *appConfig) SetConfigLocation(loc string) {
	// do nothing since we don't access any local resources
}

func (c *appConfig) Settings() *settings.DialSettings {
	if c.ds != nil {
		return c.ds
	}
	// make it available for future calls
	c.ds = c.defaultSettings()
	return c.ds
}

func (c *appConfig) defaultSettings() *settings.DialSettings {
	cfg := settings.DialSettings{
		Endpoint:      config.DefaultEndpoint,
		DefaultScopes: defaultScopes(),
		Credentials:   &settings.Credentials{}, // add this to avoid NPEs further down
	}
	// patch values from ENV, if available
	cfg.Endpoint = stdlib.GetString(config.APIEndpointENV, cfg.Endpoint)
	return &cfg
}

func defaultScopes() []string {
	// FIXME: this gives basic read access to the API. Is this what we want?
	return []string{
		auth.ScopeApiRead,
		//auth.ScopeApiWrite,
	}
}
