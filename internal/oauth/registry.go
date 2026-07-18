package oauth

import (
	"fmt"
	"net/http"
	"sort"
)

// Registry holds all configured OAuth providers, keyed by name.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry creates an empty Registry with no registered providers.
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

// Register adds a provider to the registry.
// Returns an error if a provider with the same name is already registered.
func (r *Registry) Register(p Provider) error {
	name := p.Name()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = p
	return nil
}

// Get returns the Provider registered under the given name, or nil
// if no provider with that name exists.
func (r *Registry) Get(name string) Provider {
	return r.providers[name]
}

// List returns all registered provider names sorted in alphabetical order.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ProviderConfig represents a single provider entry in the
// [[oauth.providers]] TOML configuration array.
type ProviderConfig struct {
	Name         string `toml:"name"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	AuthorizeURL string `toml:"authorize_url"`
	TokenURL     string `toml:"token_url"`
	UserinfoURL  string `toml:"userinfo_url"`
}

// OAuthConfig holds the parsed [oauth] TOML configuration section.
type OAuthConfig struct {
	Providers []ProviderConfig `toml:"providers"`
}

// BuildRegistryFromConfig creates a Registry from the given OAuth provider
// configurations and shared HTTP client. It returns an error for unknown
// provider names.
func BuildRegistryFromConfig(providers []ProviderConfig, client *http.Client) (*Registry, error) {
	registry := NewRegistry()
	for _, pc := range providers {
		switch pc.Name {
		case "github":
			p := NewGitHubProvider(
				pc.ClientID,
				pc.ClientSecret,
				pc.AuthorizeURL,
				pc.TokenURL,
				pc.UserinfoURL,
				client,
			)
			if err := registry.Register(p); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown provider: %s", pc.Name)
		}
	}
	return registry, nil
}
