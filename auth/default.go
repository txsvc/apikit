package auth

import (
	"log"
	"sync"

	"github.com/txsvc/stdlib/v2"
	"github.com/txsvc/stdlib/v2/settings"
)

type (
	defaultAuthImpl struct {
	}
)

var (
	// Interface guards.

	// This enforces a compile-time check of the provider implmentation,
	// making sure all the methods defined in the interfaces are implemented.

	_ stdlib.GenericProvider = (*defaultAuthImpl)(nil)

	_ AuthProvider = (*defaultAuthImpl)(nil)

	// the instance, a singleton
	theDefaultProvider *defaultAuthImpl

	// different types of lookup tables
	tokenToAuth map[string]*settings.DialSettings
	idToAuth    map[string]*settings.DialSettings
	mu          sync.Mutex // used to protect the above cache
)

func init() {
	// force a reset
	theDefaultProvider = nil
	tokenToAuth = make(map[string]*settings.DialSettings)
	idToAuth = make(map[string]*settings.DialSettings)

	// initialize the default in-memory only auth provider
	authConfig := stdlib.WithProvider("apikit.default.auth", TypeAuthProvider, NewDefaultProvider)

	if _, err := NewConfig(authConfig); err != nil {
		log.Fatal(err)
	}

	imp, found := authProvider.Find(TypeAuthProvider)
	if !found {
		log.Fatal(ErrInternalAuthError)
	}
	theDefaultProvider = imp.(*defaultAuthImpl)
}

// a default provider with just an in-memory store
func NewDefaultProvider() interface{} {
	return &defaultAuthImpl{}
}

func (np *defaultAuthImpl) LookupByToken(token string) (*settings.DialSettings, error) {
	//observer.LogWithLevel(observer.LevelDebug, fmt.Sprintf("lookup. t=%s", token))

	if token == "" {
		return nil, ErrNoToken
	}
	if a, ok := tokenToAuth[token]; ok {
		return a, nil
	}
	return nil, ErrTokenNotFound
}

func (np *defaultAuthImpl) UpdateStore(ds *settings.DialSettings) error {
	mu.Lock()
	defer mu.Unlock()

	if !ds.Credentials.IsValid() {
		return ErrInvalidCredentials
	}
	if len(ds.Credentials.Token) == 0 {
		return ErrInvalidCredentials
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

func (np *defaultAuthImpl) Close() error {
	return nil
}
