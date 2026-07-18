package db

// schemaStatements contains all six CREATE TABLE IF NOT EXISTS statements
// in foreign-key dependency order: parent tables before child tables.
// These are executed inside a single DEFERRED transaction by initDB.
var schemaStatements = []string{
	// 1. users — referenced by api_keys, pats, and org_members
	`CREATE TABLE IF NOT EXISTS users (
	id TEXT NOT NULL PRIMARY KEY,
	username TEXT NOT NULL UNIQUE,
	email TEXT NOT NULL,
	full_name TEXT,
	role TEXT NOT NULL DEFAULT 'user',
	status TEXT NOT NULL DEFAULT 'active',
	provider TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE (provider, provider_id)
)`,

	// 2. api_keys — FK to users(id)
	`CREATE TABLE IF NOT EXISTS api_keys (
	key_id TEXT NOT NULL PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id),
	secret_hash TEXT NOT NULL,
	expires_days INTEGER NOT NULL,
	expires_at TEXT,
	revoked_at TEXT,
	created_at TEXT NOT NULL
)`,

	// 3. pats — FK to users(id)
	`CREATE TABLE IF NOT EXISTS pats (
	token_id TEXT NOT NULL PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	secret_hash TEXT NOT NULL,
	permissions TEXT NOT NULL,
	expires_days INTEGER NOT NULL,
	expires_at TEXT,
	revoked_at TEXT,
	created_at TEXT NOT NULL
)`,

	// 4. orgs — referenced by org_members
	`CREATE TABLE IF NOT EXISTS orgs (
	id TEXT NOT NULL PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	slug TEXT NOT NULL UNIQUE,
	url TEXT,
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`,

	// 5. org_members — FK to orgs(id) ON DELETE CASCADE, FK to users(id)
	`CREATE TABLE IF NOT EXISTS org_members (
	org_id TEXT NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id),
	created_at TEXT NOT NULL,
	PRIMARY KEY (org_id, user_id)
)`,

	// 6. admin_config — standalone key-value store
	`CREATE TABLE IF NOT EXISTS admin_config (
	key TEXT NOT NULL PRIMARY KEY,
	value TEXT NOT NULL
)`,
}
