package auth

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// validIdentifier matches lowercase letters, digits, and underscores (non-empty).
var validIdentifier = regexp.MustCompile(`^[a-z0-9_]+$`)

// PermissionRegistry is a thread-safe registry of valid resource_type:action
// pairs used for PAT permission validation. Pre-populated with built-in
// permissions; extensible by consuming projects.
type PermissionRegistry struct {
	mu    sync.RWMutex
	perms map[string]struct{}
}

// NewPermissionRegistry returns a PermissionRegistry pre-populated with the
// 6 built-in permissions: users:read, orgs:read, keys:read, keys:manage,
// tokens:read, tokens:manage.
func NewPermissionRegistry() *PermissionRegistry {
	r := &PermissionRegistry{
		perms: make(map[string]struct{}),
	}
	builtins := []string{
		"users:read",
		"orgs:read",
		"keys:read",
		"keys:manage",
		"tokens:read",
		"tokens:manage",
	}
	for _, p := range builtins {
		r.perms[p] = struct{}{}
	}
	return r
}

// Register adds a resource_type:action permission pair to the registry.
// Both resourceType and action must be non-empty and contain only lowercase
// letters, digits, and underscores. Returns a non-nil error if the pair is
// invalid or already registered.
func (r *PermissionRegistry) Register(resourceType, action string) error {
	if !validIdentifier.MatchString(resourceType) {
		return fmt.Errorf("invalid resource_type %q: must be non-empty, lowercase letters/digits/underscores only", resourceType)
	}
	if !validIdentifier.MatchString(action) {
		return fmt.Errorf("invalid action %q: must be non-empty, lowercase letters/digits/underscores only", action)
	}

	key := resourceType + ":" + action

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.perms[key]; ok {
		return fmt.Errorf("permission %q is already registered", key)
	}

	r.perms[key] = struct{}{}
	return nil
}

// IsValid returns true if the resource_type:action pair is registered.
func (r *PermissionRegistry) IsValid(resourceType, action string) bool {
	key := resourceType + ":" + action
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.perms[key]
	return ok
}

// List returns all registered resource_type:action strings as a sorted
// slice in ascending lexicographic order.
func (r *PermissionRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]string, 0, len(r.perms))
	for k := range r.perms {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}
