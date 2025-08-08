package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/txsvc/stdlib/v2/settings"
)

const (
	scopeProductionRead  = "production:read"
	scopeProductionWrite = "production:write"
	scopeProductionBuild = "production:build"
	scopeResourceRead    = "resource:read"
	scopeResourceWrite   = "resource:write"
)

func TestHasScope(t *testing.T) {
	assert.True(t, hasScope([]string{scopeProductionWrite, scopeProductionRead, scopeResourceRead}, scopeProductionRead))
	assert.False(t, hasScope([]string{scopeProductionWrite, scopeProductionRead, scopeResourceRead}, scopeResourceWrite))

	assert.True(t, hasScope([]string{scopeProductionWrite, scopeProductionRead, scopeResourceRead}, scopeProductionRead+","+scopeProductionWrite))
	assert.False(t, hasScope([]string{scopeProductionWrite, scopeProductionRead, scopeResourceRead}, scopeProductionRead+","+scopeResourceWrite))
}

func TestInitAuthProvider(t *testing.T) {
	assert.NotNil(t, theDefaultProvider)
	assert.NotNil(t, authProvider)

	imp, found := authProvider.Find(TypeAuthProvider)
	assert.True(t, found)
	assert.NotNil(t, imp)
}

func TestUpdateStoreInvalidCredentials(t *testing.T) {
	// just empty
	ds := settings.DialSettings{
		Credentials: &settings.Credentials{},
	}
	err := UpdateStore(&ds)
	assert.Error(t, err)

	// valid but no token
	ds = settings.DialSettings{
		Credentials: &settings.Credentials{
			ClientID:     "client",
			ClientSecret: "secret",
		},
	}
	err = UpdateStore(&ds)
	assert.Error(t, err)
}

func TestUpdateStore(t *testing.T) {
	ds := settings.DialSettings{
		Credentials: &settings.Credentials{
			ClientID: "client",
			Token:    "token",
		},
	}
	err := UpdateStore(&ds)
	assert.NoError(t, err)

	// test idempotency
	ds2 := ds.Clone()
	err = UpdateStore(&ds2)
	assert.NoError(t, err)
}

func TestLookupByToken(t *testing.T) {
	ds, err := LookupByToken("token")
	assert.NoError(t, err)
	assert.NotNil(t, ds)
	assert.Equal(t, "token", ds.Credentials.Token)
}

func TestLookupByTokenFail(t *testing.T) {
}
