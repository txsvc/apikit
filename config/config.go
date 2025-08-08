package config

import (
	"errors"
	"log"

	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"
)

const (
	PortENV              = "PORT"            // runtime settings
	ConfigDirLocationENV = "CONFIG_LOCATION" // config settings
	AppSessionKeyENV     = "APP_SESSION_KEY" // Session/Auth key used to encrypt cookies with

	APIEndpointENV = "API_ENDPOINT" // client settings
	ForceTraceENV  = "API_FORCE_TRACE"

	// Other constants
	DefaultConfigName          = "config"
	DefaultConfigLocation      = "./.config"
	DefaultCredentialsLocation = "./.secrets"
	DefaultEndpoint            = "http://localhost:8080" // only really useful for testing ...
)

type (
	// Info holds static information about a service or API
	Info struct {
		// name: the service's name in human-usable form
		name string
		// shortName: the abreviated version of the service's name
		shortName string
		// copyright: info on the copyright/owner of the service/api
		copyright string
		// about: a short description of the service/api
		about string
		// majorVersion: the major version of the service/api
		majorVersion int
		// minorVersion: the minor version of the service/api
		minorVersion int
		// fixVersion: the fix/patch version of the service/api
		fixVersion int
	}

	ConfigProvider interface {
		// AppInfo returns static information about the app or service
		Info() *Info
		// Settings returns the app settings, if configured, or falls back to a default, minimal configuration
		Settings() *settings.DialSettings
		// ConfigLocation returns the path to the config location, if set, or the default location otherwise.
		ConfigLocation() string // './.config' unless explicitly set.
		// SetConfigLocation explicitly sets the location where the configuration is expected. The location's existence is NOT verified.
		SetConfigLocation(string)
	}
)

var (
	// ErrMissingConfigurator indicates that the config package is not initialized
	ErrMissingConfigurator = errors.New("missing configurator")
	// ErrInitializingConfiguration indicates that the client could not be initialized
	ErrInitializingConfiguration = errors.New("error initializing configuration")
	// ErrInvalidConfiguration indicates that parameters used to configure the service were invalid
	ErrInvalidConfiguration = errors.New("invalid configuration")

	// the config "singleton"
	config_ ConfigProvider

	// the current session key
	sessionKey = stdlib.GetString(AppSessionKeyENV, stdlib.RandStringSimple(128))
)

func init() {
	// makes sure that SOMETHING is initialized
	SetProvider(NewLocalConfigProvider())
}

func SetProvider(provider ConfigProvider) {
	config_ = provider
}

func GetConfig() ConfigProvider {
	return config_
}

// SetConfigLocation sets the actual location without checking if the location actually exists !
func SetConfigLocation(loc string) {
	if config_ == nil {
		log.Fatal(ErrMissingConfigurator)
	}
	config_.SetConfigLocation(loc)
}

// AppSessionKey is initialized from ENV['APP_SESSION_KEY'] or randomly generated on startup, if not provided.
func AppSessionKey() string {
	return sessionKey
}
