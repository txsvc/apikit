package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"

	"github.com/txsvc/apikit/auth"
)

// the below version numbers should match the git release tags,
// i.e. there should be a version 'v0.1.0' on branch main !
const (
	majorVersion = 0
	minorVersion = 1
	fixVersion   = 0
)

type (
	localConfig struct {
		// the interface to implement
		ConfigProvider

		// app info
		info *Info
		// path to configuration settings
		rootDir string // the current working dir
		confDir string // the fully qualified path to the conf dir
		// cached settings
		ds *settings.DialSettings
	}
)

func NewLocalConfigProvider() ConfigProvider {

	// get the current working dir. abort on error
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	c := &localConfig{
		rootDir: dir,
		confDir: "",
		info: &Info{
			name:         "appkit",
			shortName:    "appkit",
			copyright:    "Copyright 2022, transformative.services, https://txs.vc",
			about:        "about appkit",
			majorVersion: majorVersion,
			minorVersion: minorVersion,
			fixVersion:   fixVersion,
		},
	}

	return c
}

func (c *localConfig) Info() *Info {
	return c.info
}

// ConfigLocation returns the config location that was set using SetConfigLocation().
// If no location is defined, GetConfigLocation looks for ENV['CONFIG_LOCATION'] or
// returns DefaultConfigLocation() if no environment variable was set.
func (c *localConfig) ConfigLocation() string {
	if len(c.confDir) == 0 {
		return stdlib.GetString(ConfigDirLocationENV, DefaultConfigLocation)
	}
	return c.confDir
}

func (c *localConfig) SetConfigLocation(loc string) {
	c.confDir = loc
	if c.ds != nil {
		c.ds = nil // force a reload the next time GetSettings() is called ...
	}
}

func (c *localConfig) Settings() *settings.DialSettings {
	if c.ds != nil {
		return c.ds
	}

	// try to load the dial settings
	pathToFile := filepath.Join(c.ConfigLocation(), DefaultConfigName)
	cfg, err := settings.ReadDialSettings(pathToFile)
	if err != nil {
		cfg = c.defaultSettings()
		// save to the default location
		if err = settings.WriteDialSettings(cfg, pathToFile); err != nil {
			log.Fatal(err)
		}
	}

	// patch values from ENV, if available
	cfg.Endpoint = stdlib.GetString(APIEndpointENV, cfg.Endpoint)

	// make it available for future calls
	c.ds = cfg
	return c.ds
}

func (c *localConfig) defaultSettings() *settings.DialSettings {

	return &settings.DialSettings{
		Endpoint:      DefaultEndpoint,
		Credentials:   &settings.Credentials{}, // add this to avoid NPEs further down
		DefaultScopes: defaultScopes(),
		UserAgent:     c.info.UserAgentString(),
	}
}

func defaultScopes() []string {
	// FIXME: this gives basic read access to the API. Is this what we want?
	return []string{
		auth.ScopeApiRead,
	}
}
