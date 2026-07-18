//go:build ignore

// This file is intentionally excluded from normal builds (go:build ignore).
// It verifies TS-06-E1 (06-REQ-1.E1): a type implementing only a subset of
// the Provider interface methods fails to compile.
//
// To verify: attempt to build this file directly:
//   go build -tags ignore ./internal/oauth/incomplete_provider_test.go
//
// Expected result: compilation fails with missing-method errors for
// Exchange and UserInfo.

package oauth_test

import "github.com/txsvc/apikit/internal/oauth"

// incompleteProvider implements only Name() and AuthorizeURL(),
// missing Exchange() and UserInfo().
type incompleteProvider struct{}

func (p *incompleteProvider) Name() string                          { return "incomplete" }
func (p *incompleteProvider) AuthorizeURL(state, redirect string) string { return "" }

// This line must NOT compile — incompleteProvider is missing Exchange and UserInfo.
var _ oauth.Provider = &incompleteProvider{}
