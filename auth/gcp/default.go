package gcp

import (
	"log"
	"sync"

	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"

	"github.com/txsvc/apikit/auth"
)

type (
	defaultGCPAuthImpl struct {
	}
)

var (
	// Interface guards.

	// This enforces a compile-time check of the provider implmentation,
	// making sure all the methods defined in the interfaces are implemented.

	_ stdlib.GenericProvider = (*defaultGCPAuthImpl)(nil)

	_ auth.AuthProvider = (*defaultGCPAuthImpl)(nil)

	// different types of lookup tables
	tokenToAuth map[string]*settings.DialSettings
	idToAuth    map[string]*settings.DialSettings
	mu          sync.Mutex // used to protect the above cache
)

func init() {
	// force a reset
	tokenToAuth = make(map[string]*settings.DialSettings)
	idToAuth = make(map[string]*settings.DialSettings)

	// initialize the Google Cloud Store backed provider
	authConfig := stdlib.WithProvider("apikit.gcp.auth", auth.TypeAuthProvider, NewGCPProvider)
	authProvider, err := auth.NewConfig(authConfig)
	if err != nil {
		log.Fatal(err)
	}

	_, found := authProvider.Find(auth.TypeAuthProvider)
	if !found {
		log.Fatal(auth.ErrInternalAuthError)
	}
}

// a default provider with just an in-memory store
func NewGCPProvider() interface{} {
	return &defaultGCPAuthImpl{}
}

func (np *defaultGCPAuthImpl) LookupByToken(token string) (*settings.DialSettings, error) {
	//observer.LogWithLevel(observer.LevelDebug, fmt.Sprintf("lookup. t=%s", token))

	if token == "" {
		return nil, auth.ErrNoToken
	}
	if a, ok := tokenToAuth[token]; ok {
		return a, nil
	}
	return nil, auth.ErrTokenNotFound
}

func (np *defaultGCPAuthImpl) UpdateStore(ds *settings.DialSettings) error {
	mu.Lock()
	defer mu.Unlock()

	if !ds.Credentials.IsValid() {
		return auth.ErrInvalidCredentials
	}
	if len(ds.Credentials.Token) == 0 {
		return auth.ErrInvalidCredentials
	}

	//observer.LogWithLevel(observer.LevelDebug, fmt.Sprintf("update credentials. t=%s/%s", ds.Credentials.ClientID, ds.Credentials.Token))

	/*
		// check if the settings already exists
		if a, ok := np.idToAuth[ds.Credentials.Key()]; ok {
			// FIXME this needs a change in the flow/logic of the resource_auth.go !

				if a.Credentials.Status == settings.StateAuthorized {
					_ = observer.ReportError(fmt.Errorf("already authorized. t=%s, state=%d", a.Credentials.Token, a.Credentials.Status))
					return ErrAlreadyAuthorized
				}

			// remove from token lookup if the token changed
			if a.Credentials.Token != ds.Credentials.Token {
				delete(np.tokenToAuth, a.Credentials.Token)
			}
		}
	*/

	// update to the cache
	_ds := ds.Clone()
	tokenToAuth[ds.Credentials.Token] = &_ds
	idToAuth[ds.Credentials.Key()] = &_ds

	return nil
}

func (np *defaultGCPAuthImpl) Close() error {
	return nil
}
