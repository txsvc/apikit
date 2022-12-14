package auth

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/txsvc/stdlib/v2"

	"github.com/txsvc/apikit/helpers"
	"github.com/txsvc/apikit/settings"
)

type (
	authCache struct {
		root string // location on disc
		// different types of lookup tables
		tokenToAuth map[string]*settings.DialSettings
		idToAuth    map[string]*settings.DialSettings
	}
)

var (
	cache *authCache // authorization cache
	mu    sync.Mutex // used to protect the above cache
)

func FlushAuthorizations(root string) {
	mu.Lock()
	defer mu.Unlock()

	_log.Debugf("flushing auth cache. root=%s", root)

	cache = &authCache{
		root:        root,
		tokenToAuth: make(map[string]*settings.DialSettings),
		idToAuth:    make(map[string]*settings.DialSettings),
	}

	if root != "" {
		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if info == nil {
				return nil
			}

			if !info.IsDir() {
				cfg, err := helpers.ReadDialSettings(path)
				if err != nil {
					return err // FIXME: this is never checked on exit !
				}
				cache.Register(cfg)
			}
			return nil
		})
	}
}

func RegisterAuthorization(ds *settings.DialSettings) error {
	return cache.Register(ds)
}

func LookupByToken(token string) (*settings.DialSettings, error) {
	return cache.LookupByToken(token)
}

func UpdateStore(ds *settings.DialSettings) error {
	if _, err := cache.LookupByToken(ds.Credentials.Token); err != nil {
		return err // only allow to write already registered settings
	}
	return cache.writeToStore(ds)
}

func (c *authCache) Register(ds *settings.DialSettings) error {

	_log.Debugf("register. t=%s/%s", ds.Credentials.Token, fileName(ds.Credentials))

	// check if the settings already exists
	if a, ok := c.idToAuth[ds.Credentials.Key()]; ok {
		if a.Status == settings.StateAuthorized {
			_log.Errorf("already authorized. t=%s, state=%d", a.Credentials.Token, a.Status)
			return ErrAlreadyAuthorized
		}

		// remove from token lookup if the token changed
		if a.Credentials.Token != ds.Credentials.Token {
			delete(c.tokenToAuth, a.Credentials.Token)
		}
	}

	// write to the file store
	path := filepath.Join(c.root, fileName(ds.Credentials))
	if err := helpers.WriteDialSettings(ds, path); err != nil {
		return err
	}

	// update to the cache
	c.tokenToAuth[ds.Credentials.Token] = ds
	c.idToAuth[ds.Credentials.Key()] = ds

	return nil
}

func (c *authCache) LookupByToken(token string) (*settings.DialSettings, error) {
	_log.Debugf("lookup. t=%s", token)

	if token == "" {
		return nil, ErrNoToken
	}
	if a, ok := c.tokenToAuth[token]; ok {
		return a, nil
	}
	return nil, nil // FIXME: return an error ?
}

func (c *authCache) writeToStore(ds *settings.DialSettings) error {
	// write to the file store
	path := filepath.Join(c.root, fileName(ds.Credentials))
	if err := helpers.WriteDialSettings(ds, path); err != nil {
		return err
	}
	return nil
}

func fileName(cred *settings.Credentials) string {
	return stdlib.Fingerprint(cred.Key())
}
