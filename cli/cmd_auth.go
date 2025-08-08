package cli

import (
	"fmt"
	"path/filepath"

	"github.com/urfave/cli/v2"

	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"

	"github.com/txsvc/apikit/api"
	"github.com/txsvc/apikit/auth"
	"github.com/txsvc/apikit/config"
)

func WithAuthCommands() []*cli.Command {
	return []*cli.Command{
		{
			Name:    "auth",
			Aliases: []string{"a"},
			Usage:   "options to register and login",
			Subcommands: []*cli.Command{
				{
					Name:        "init",
					Usage:       "register with the API service",
					UsageText:   "init email [passphrase]", // FIXME: better description
					Description: "longform description",    // FIXME: better description
					Action:      InitCommand,
				},
				{
					Name:        "login",
					Usage:       "authenticate with the API service",
					UsageText:   "login token",          // FIXME: better description
					Description: "longform description", // FIXME: better description
					Action:      LoginCommand,
				},
				{
					Name:        "logout",
					Usage:       "logout from the API service",
					UsageText:   "logout",               // FIXME: better description
					Description: "longform description", // FIXME: better description
					Action:      LogoutCommand,
				},
			},
		},
	}
}

func InitCommand(c *cli.Context) error {
	if c.NArg() < 1 || c.NArg() > 2 {
		return ErrInvalidNumArguments
	}

	userid := ""
	phrase := ""

	if c.NArg() == 1 {
		userid = c.Args().First() // path only
	} else if c.NArg() == 2 {
		userid = c.Args().First() // path and pass phrase
		phrase = c.Args().Get(1)
	}

	// create or validate the words
	mnemonic, err := stdlib.CreateMnemonic(phrase)
	if err != nil {
		return err
	}

	// load settings
	cfg := config.GetConfig().Settings()

	// get a client instance
	cl := api.NewClient(cfg)
	if cl == nil {
		return fmt.Errorf("could not create client")
	}

	// if a passphrase was provided and the fingerprint(realm,userid,phrase) matches the API key,
	// then the user is re-initializing an existing account which is allowed. The client just
	// sends a logout request first before initiating the normal auth sequence.

	_apiKey := stdlib.Fingerprint(fmt.Sprintf("%s%s%s", config.GetConfig().Info().Name(), userid, mnemonic))

	switch cfg.Credentials.Status {
	case -1:
		// set to INVALID
		return config.ErrInvalidConfiguration
	case 1:
		if _apiKey == cfg.GetOption("APIKey") {
			// correct pass phrase was provided, reset the authentication
			if err := cl.LogoutCommand(); err != nil {
				return err // FIXME: better err or just pass on what comes?
			}
		} else {
			// already authenticated, abort
			return auth.ErrAlreadyAuthorized
		}
	}

	// 0, -2: don't care, can be overwritten as the client is not authorized yet

	cfg.Credentials = &settings.Credentials{
		ProjectID: config.GetConfig().Info().Name(),
		ClientID:  userid,
		Token:     api.CreateSimpleToken(),
		Expires:   0, // FIXME: should this expire after some time?
	}
	cfg.Credentials.Status = api.StateInit
	cfg.SetOption("APIKey", _apiKey)
	cfg.Scopes = make([]string, 0)
	cfg.DefaultScopes = make([]string, 0)
	cfg.Options = make(map[string]string)

	// now start the auth init process with the API

	err = cl.InitCommand(cfg)
	if err != nil {
		return err // FIXME: better err or just pass on what comes?
	}

	// finally save the file
	pathToFile := filepath.Join(config.GetConfig().ConfigLocation(), config.DefaultConfigName)
	if err := settings.WriteDialSettings(cfg, pathToFile); err != nil {
		return config.ErrInitializingConfiguration
	}

	if phrase == "" {
		fmt.Printf("userid: %s\n", cfg.Credentials.ClientID)
		fmt.Printf("passphrase: \"%s\"\n\n", mnemonic)
		fmt.Println("Make a copy of the passphrase and keep it secure !")
	}

	return nil
}

func LoginCommand(c *cli.Context) error {
	if c.NArg() < 1 || c.NArg() > 1 {
		return ErrInvalidNumArguments
	}

	token := c.Args().First()

	// load settings
	cfg := config.GetConfig().Settings()
	if !cfg.Credentials.IsValid() {
		return config.ErrInvalidConfiguration
	}

	// now start the auth login process with the API
	cl := api.NewClient(cfg)
	if cl == nil {
		return fmt.Errorf("could not create client")
	}

	status, err := cl.LoginCommand(token)
	if err != nil {
		return err // FIXME: better err or just pass on what comes?
	}

	// update the local config
	cfg.Credentials.Token = status.Message
	cfg.Credentials.Status = api.StateAuthorized // LOGGED_IN
	if !cfg.Credentials.IsValid() {
		return config.ErrInvalidConfiguration
	}

	pathToFile := filepath.Join(config.GetConfig().ConfigLocation(), config.DefaultConfigName)
	if err := settings.WriteDialSettings(cfg, pathToFile); err != nil {
		return config.ErrInitializingConfiguration
	}

	fmt.Println("auth login done") // FIXME: better message !

	return nil
}

func LogoutCommand(c *cli.Context) error {
	if c.NArg() > 0 {
		return ErrInvalidNumArguments
	}

	// load settings
	cfg := config.GetConfig().Settings()
	if !cfg.Credentials.IsValid() {
		return config.ErrInvalidConfiguration
	}

	// now start the auth logout process with the API
	cl := api.NewClient(cfg)
	if cl == nil {
		return fmt.Errorf("could not create client")
	}
	err := cl.LogoutCommand()
	if err != nil {
		return err // FIXME: better err or just pass on what comes?
	}

	// update the local config
	cfg.Credentials.Expires = stdlib.Now() - 1
	cfg.Credentials.Status = api.StateUndefined // LOGGED_OUT

	pathToFile := filepath.Join(config.GetConfig().ConfigLocation(), config.DefaultConfigName)
	if err := settings.WriteDialSettings(cfg, pathToFile); err != nil {
		return config.ErrInitializingConfiguration
	}

	fmt.Println("auth logout done") // FIXME: better message !

	return nil
}
