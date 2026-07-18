package cli_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	apikit "github.com/txsvc/apikit"
	"github.com/txsvc/apikit/internal/cli"
)

// ---------------------------------------------------------------------------
// Test-scoped mock interface for SDK org admin methods.
//
// This interface is UNEXPORTED and defined only in this _test.go file.
// It covers all apikit.Client methods that org admin commands will call.
//
// DIVERGENCE: The SDK's BlockOrg/UnblockOrg currently return only error,
// but the spec (14-REQ-16.1, 14-REQ-17.1) says these commands "print the
// returned apikit.Organization as JSON." The mock uses
// (*apikit.Organization, error) to match the spec's intended behavior.
// The production code may call the SDK action method followed by GetOrg,
// or the SDK may be updated.
//
// DIVERGENCE: The SDK's CreateOrg/UpdateOrg return *Response[Organization],
// but the spec says to print the Organization directly. The mock returns
// *apikit.Organization to match the spec's intent; the production RunE
// would unwrap .Data from the Response wrapper.
// ---------------------------------------------------------------------------

type adminOrgsClient interface {
	ListOrgs(ctx context.Context, opts *apikit.ListOrgsOptions) ([]*apikit.Organization, error)
	CreateOrg(ctx context.Context, req *apikit.CreateOrgRequest) (*apikit.Organization, error)
	UpdateOrg(ctx context.Context, id string, req *apikit.UpdateOrgRequest) (*apikit.Organization, error)
	DeleteOrg(ctx context.Context, id string) error
	BlockOrg(ctx context.Context, id string) (*apikit.Organization, error)
	UnblockOrg(ctx context.Context, id string) (*apikit.Organization, error)
	ListOrgMembers(ctx context.Context, orgID string) ([]*apikit.User, error)
	AddOrgMember(ctx context.Context, orgID, userID string) error
	RemoveOrgMember(ctx context.Context, orgID, userID string) error
}

// mockAdminOrgsClient implements adminOrgsClient for testing.
type mockAdminOrgsClient struct {
	// Return values
	listOrgsResult   []*apikit.Organization
	listOrgsErr      error
	createOrgResult  *apikit.Organization
	createOrgErr     error
	updateOrgResult  *apikit.Organization
	updateOrgErr     error
	deleteOrgErr     error
	blockOrgResult   *apikit.Organization
	blockOrgErr      error
	unblockOrgResult *apikit.Organization
	unblockOrgErr    error
	listMembersResult []*apikit.User
	listMembersErr    error
	addMemberErr      error
	removeMemberErr   error

	// Call tracking
	listOrgsCalled  bool
	listOrgsOpts    *apikit.ListOrgsOptions
	createOrgCalled bool
	createOrgReq    *apikit.CreateOrgRequest
	updateOrgCalled bool
	updateOrgID     string
	updateOrgReq    *apikit.UpdateOrgRequest
	deleteOrgCalled bool
	deleteOrgID     string
	blockOrgCalled  bool
	blockOrgID      string
	unblockOrgCalled bool
	unblockOrgID     string
	listMembersCalled bool
	listMembersOrgID  string
	addMemberCalled   bool
	addMemberOrgID    string
	addMemberUserID   string
	removeMemberCalled bool
	removeMemberOrgID  string
	removeMemberUserID string

	// Context tracking
	capturedCtx context.Context
}

func (m *mockAdminOrgsClient) ListOrgs(ctx context.Context, opts *apikit.ListOrgsOptions) ([]*apikit.Organization, error) {
	m.capturedCtx = ctx
	m.listOrgsCalled = true
	m.listOrgsOpts = opts
	return m.listOrgsResult, m.listOrgsErr
}

func (m *mockAdminOrgsClient) CreateOrg(ctx context.Context, req *apikit.CreateOrgRequest) (*apikit.Organization, error) {
	m.capturedCtx = ctx
	m.createOrgCalled = true
	m.createOrgReq = req
	return m.createOrgResult, m.createOrgErr
}

func (m *mockAdminOrgsClient) UpdateOrg(ctx context.Context, id string, req *apikit.UpdateOrgRequest) (*apikit.Organization, error) {
	m.capturedCtx = ctx
	m.updateOrgCalled = true
	m.updateOrgID = id
	m.updateOrgReq = req
	return m.updateOrgResult, m.updateOrgErr
}

func (m *mockAdminOrgsClient) DeleteOrg(ctx context.Context, id string) error {
	m.capturedCtx = ctx
	m.deleteOrgCalled = true
	m.deleteOrgID = id
	return m.deleteOrgErr
}

func (m *mockAdminOrgsClient) BlockOrg(ctx context.Context, id string) (*apikit.Organization, error) {
	m.capturedCtx = ctx
	m.blockOrgCalled = true
	m.blockOrgID = id
	return m.blockOrgResult, m.blockOrgErr
}

func (m *mockAdminOrgsClient) UnblockOrg(ctx context.Context, id string) (*apikit.Organization, error) {
	m.capturedCtx = ctx
	m.unblockOrgCalled = true
	m.unblockOrgID = id
	return m.unblockOrgResult, m.unblockOrgErr
}

func (m *mockAdminOrgsClient) ListOrgMembers(ctx context.Context, orgID string) ([]*apikit.User, error) {
	m.capturedCtx = ctx
	m.listMembersCalled = true
	m.listMembersOrgID = orgID
	return m.listMembersResult, m.listMembersErr
}

func (m *mockAdminOrgsClient) AddOrgMember(ctx context.Context, orgID, userID string) error {
	m.capturedCtx = ctx
	m.addMemberCalled = true
	m.addMemberOrgID = orgID
	m.addMemberUserID = userID
	return m.addMemberErr
}

func (m *mockAdminOrgsClient) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	m.capturedCtx = ctx
	m.removeMemberCalled = true
	m.removeMemberOrgID = orgID
	m.removeMemberUserID = userID
	return m.removeMemberErr
}

// ===========================================================================
// Task Group 3.1: admin orgs list and create tests (REQ-12, REQ-13)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-36: akc admin orgs list without --include-blocked calls ListOrgs
// with IncludeBlocked:false and prints the org array as JSON.
// Requirement: 14-REQ-12.1
// ---------------------------------------------------------------------------

func TestAdminOrgsListCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		listOrgsResult: []*apikit.Organization{{ID: "o1", Name: "Acme", Slug: "acme"}},
	}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock), "orgs", "list")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	// ListOrgs should have been called with IncludeBlocked=false.
	if !mock.listOrgsCalled {
		t.Fatal("ListOrgs was not called")
	}
	if mock.listOrgsOpts == nil {
		t.Fatal("ListOrgs options is nil")
	}
	if mock.listOrgsOpts.IncludeBlocked {
		t.Error("ListOrgs called with IncludeBlocked=true, want false")
	}

	// stdout should be a JSON array with one org.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var orgs []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &orgs); err != nil {
		t.Fatalf("failed to parse stdout as JSON array: %v", err)
	}
	if len(orgs) != 1 {
		t.Errorf("got %d orgs, want 1", len(orgs))
	}
	if id, _ := orgs[0]["id"].(string); id != "o1" {
		t.Errorf("org[0].id = %q, want %q", id, "o1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-37: akc admin orgs list --include-blocked calls ListOrgs with
// IncludeBlocked:true.
// Requirement: 14-REQ-12.2
// ---------------------------------------------------------------------------

func TestAdminOrgsListIncludeBlocked(t *testing.T) {
	mock := &mockAdminOrgsClient{
		listOrgsResult: []*apikit.Organization{{ID: "o1", Status: "blocked"}},
	}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock), "orgs", "list", "--include-blocked")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.listOrgsCalled {
		t.Fatal("ListOrgs was not called")
	}
	if mock.listOrgsOpts == nil {
		t.Fatal("ListOrgs options is nil")
	}
	if !mock.listOrgsOpts.IncludeBlocked {
		t.Error("ListOrgs called with IncludeBlocked=false, want true")
	}

	if !json.Valid([]byte(stdout)) {
		t.Errorf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-38: akc admin orgs list is registered in the agent interface with
// method GET, path /orgs, auth admin, and flag --include-blocked.
// Requirement: 14-REQ-12.3
// ---------------------------------------------------------------------------

func TestAdminOrgsListAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	// Find the "orgs list" subcommand.
	listCmd, _, err := cmd.Find([]string{"orgs", "list"})
	if err != nil {
		t.Fatalf("failed to find 'orgs list' command: %v", err)
	}
	if listCmd.Name() != "list" {
		t.Fatalf("found command %q, want %q", listCmd.Name(), "list")
	}

	// Verify annotations contain the expected metadata.
	annotations := listCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs list command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/orgs" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify the --include-blocked flag is registered with default false.
	flag := listCmd.Flags().Lookup("include-blocked")
	if flag == nil {
		t.Error("--include-blocked flag not registered on orgs list command")
	} else {
		if flag.DefValue != "false" {
			t.Errorf("--include-blocked default = %q, want %q", flag.DefValue, "false")
		}
	}
}

// ---------------------------------------------------------------------------
// TS-14-39: akc admin orgs create with --name, --slug, --url calls
// CreateOrg with a non-nil URL pointer and prints the org as JSON.
// Requirement: 14-REQ-13.1
// ---------------------------------------------------------------------------

func TestAdminOrgsCreateCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		createOrgResult: &apikit.Organization{ID: "o1", Name: "Acme", Slug: "acme"},
	}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock),
		"orgs", "create",
		"--name", "Acme",
		"--slug", "acme",
		"--url", "https://acme.com",
	)

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.createOrgCalled {
		t.Fatal("CreateOrg was not called")
	}
	if mock.createOrgReq == nil {
		t.Fatal("CreateOrg request is nil")
	}
	if mock.createOrgReq.Name != "Acme" {
		t.Errorf("req.Name = %q, want %q", mock.createOrgReq.Name, "Acme")
	}
	if mock.createOrgReq.Slug != "acme" {
		t.Errorf("req.Slug = %q, want %q", mock.createOrgReq.Slug, "acme")
	}
	if mock.createOrgReq.URL == nil {
		t.Fatal("req.URL is nil, want non-nil pointer")
	}
	if *mock.createOrgReq.URL != "https://acme.com" {
		t.Errorf("*req.URL = %q, want %q", *mock.createOrgReq.URL, "https://acme.com")
	}

	// stdout should be a JSON org object.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var org map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &org); err != nil {
		t.Fatalf("failed to parse stdout as JSON object: %v", err)
	}
	if id, _ := org["id"].(string); id != "o1" {
		t.Errorf("org.id = %q, want %q", id, "o1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-40: akc admin orgs create without --url sets the URL field to nil
// in the CreateOrgRequest.
// Requirement: 14-REQ-13.2
// ---------------------------------------------------------------------------

func TestAdminOrgsCreateWithoutURL(t *testing.T) {
	mock := &mockAdminOrgsClient{
		createOrgResult: &apikit.Organization{ID: "o1", Name: "Acme", Slug: "acme"},
	}

	_, err := executeAdminCmdWithClient(makeOrgsRunner(mock),
		"orgs", "create",
		"--name", "Acme",
		"--slug", "acme",
	)

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.createOrgCalled {
		t.Fatal("CreateOrg was not called")
	}
	if mock.createOrgReq == nil {
		t.Fatal("CreateOrg request is nil")
	}
	if mock.createOrgReq.URL != nil {
		t.Errorf("req.URL = %v, want nil (--url flag was omitted)", mock.createOrgReq.URL)
	}
}

// ---------------------------------------------------------------------------
// TS-14-41: akc admin orgs create is registered in the agent interface with
// method POST, path /orgs, auth admin, required flags name/slug and
// optional flag url.
// Requirement: 14-REQ-13.3
// ---------------------------------------------------------------------------

func TestAdminOrgsCreateAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	createCmd, _, err := cmd.Find([]string{"orgs", "create"})
	if err != nil {
		t.Fatalf("failed to find 'orgs create' command: %v", err)
	}
	if createCmd.Name() != "create" {
		t.Fatalf("found command %q, want %q", createCmd.Name(), "create")
	}

	annotations := createCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs create command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/orgs" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify required flags are registered.
	nameFlag := createCmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("--name flag not registered on orgs create command")
	}
	slugFlag := createCmd.Flags().Lookup("slug")
	if slugFlag == nil {
		t.Error("--slug flag not registered on orgs create command")
	}

	// Verify optional flag is registered.
	urlFlag := createCmd.Flags().Lookup("url")
	if urlFlag == nil {
		t.Error("--url flag not registered on orgs create command")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E16: akc admin orgs create without --name exits with code 2 and
// prints the missing-flag error envelope.
// Requirement: 14-REQ-13.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsCreateMissingName(t *testing.T) {
	mock := &mockAdminOrgsClient{}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock),
		"orgs", "create",
		"--slug", "acme",
	)

	if err == nil {
		t.Error("expected error when --name is missing")
	}

	if mock.createOrgCalled {
		t.Error("CreateOrg was called despite missing --name flag")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --name" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --name")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E17: akc admin orgs create without --slug exits with code 2 and
// prints the missing-flag error envelope.
// Requirement: 14-REQ-13.E2
// ---------------------------------------------------------------------------

func TestAdminOrgsCreateMissingSlug(t *testing.T) {
	mock := &mockAdminOrgsClient{}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock),
		"orgs", "create",
		"--name", "Acme",
	)

	if err == nil {
		t.Error("expected error when --slug is missing")
	}

	if mock.createOrgCalled {
		t.Error("CreateOrg was called despite missing --slug flag")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required flag: --slug" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required flag: --slug")
	}
}

// ===========================================================================
// Task Group 3.2: admin orgs update tests (REQ-14)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-42: akc admin orgs update <id> with --name and --url calls
// UpdateOrg with non-nil pointer fields and prints the org as JSON.
// Requirement: 14-REQ-14.1
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		updateOrgResult: &apikit.Organization{ID: "o1", Name: "NewName"},
	}

	stdout, err := executeAdminCmdWithClient(makeOrgsRunner(mock),
		"orgs", "update", "o1",
		"--name", "NewName",
		"--url", "https://new.com",
	)

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.updateOrgCalled {
		t.Fatal("UpdateOrg was not called")
	}
	if mock.updateOrgID != "o1" {
		t.Errorf("captured ID = %q, want %q", mock.updateOrgID, "o1")
	}
	if mock.updateOrgReq == nil {
		t.Fatal("UpdateOrg request is nil")
	}
	if mock.updateOrgReq.Name == nil {
		t.Fatal("req.Name is nil, want non-nil pointer")
	}
	if *mock.updateOrgReq.Name != "NewName" {
		t.Errorf("*req.Name = %q, want %q", *mock.updateOrgReq.Name, "NewName")
	}
	if mock.updateOrgReq.URL == nil {
		t.Fatal("req.URL is nil, want non-nil pointer")
	}
	if *mock.updateOrgReq.URL != "https://new.com" {
		t.Errorf("*req.URL = %q, want %q", *mock.updateOrgReq.URL, "https://new.com")
	}

	// stdout should be a JSON org object.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var org map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &org); err != nil {
		t.Fatalf("failed to parse stdout as JSON object: %v", err)
	}
	if id, _ := org["id"].(string); id != "o1" {
		t.Errorf("org.id = %q, want %q", id, "o1")
	}
}

// ---------------------------------------------------------------------------
// TestAdminOrgsUpdateNameOnly: only --name provided, assert req.Name is
// non-nil ptr, req.URL is nil.
// Supplements TS-14-42 (14-REQ-14.1) with partial-update coverage.
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateNameOnly(t *testing.T) {
	mock := &mockAdminOrgsClient{
		updateOrgResult: &apikit.Organization{ID: "o1", Name: "NewName"},
	}

	_, err := executeAdminCmdWithClient(makeOrgsRunner(mock), "orgs", "update", "o1", "--name", "NewName")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.updateOrgCalled {
		t.Fatal("UpdateOrg was not called")
	}
	if mock.updateOrgReq == nil {
		t.Fatal("UpdateOrg request is nil")
	}
	if mock.updateOrgReq.Name == nil {
		t.Fatal("req.Name is nil, want non-nil pointer")
	}
	if *mock.updateOrgReq.Name != "NewName" {
		t.Errorf("*req.Name = %q, want %q", *mock.updateOrgReq.Name, "NewName")
	}
	if mock.updateOrgReq.URL != nil {
		t.Errorf("req.URL = %v, want nil (--url not provided)", mock.updateOrgReq.URL)
	}
}

// ---------------------------------------------------------------------------
// TestAdminOrgsUpdateURLOnly: only --url provided, assert req.Name is nil,
// req.URL is non-nil ptr.
// Supplements TS-14-42 (14-REQ-14.1) with partial-update coverage.
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateURLOnly(t *testing.T) {
	mock := &mockAdminOrgsClient{
		updateOrgResult: &apikit.Organization{ID: "o1"},
	}

	_, err := executeAdminCmdWithClient(makeOrgsRunner(mock), "orgs", "update", "o1", "--url", "https://new.com")

	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	if !mock.updateOrgCalled {
		t.Fatal("UpdateOrg was not called")
	}
	if mock.updateOrgReq == nil {
		t.Fatal("UpdateOrg request is nil")
	}
	if mock.updateOrgReq.Name != nil {
		t.Errorf("req.Name = %v, want nil (--name not provided)", mock.updateOrgReq.Name)
	}
	if mock.updateOrgReq.URL == nil {
		t.Fatal("req.URL is nil, want non-nil pointer")
	}
	if *mock.updateOrgReq.URL != "https://new.com" {
		t.Errorf("*req.URL = %q, want %q", *mock.updateOrgReq.URL, "https://new.com")
	}
}

// ---------------------------------------------------------------------------
// TS-14-43: akc admin orgs update with no flags emits a warnf warning to
// stderr, still calls UpdateOrg with nil fields, prints org JSON, and
// exits 0.
// Requirement: 14-REQ-14.2
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateNoFlags(t *testing.T) {
	mock := &mockAdminOrgsClient{
		updateOrgResult: &apikit.Organization{ID: "o1"},
	}

	// Capture both stdout and stderr by executing via the full command tree.
	cmd := cli.NewAdminCmd()
	stdoutBuf := new(strings.Builder)
	stderrBuf := new(strings.Builder)
	cmd.SetOut(stdoutBuf)
	cmd.SetErr(stderrBuf)
	cmd.SetArgs([]string{"orgs", "update", "o1"})

	// Inject the mock client into the command's context.
	ctx := cli.ContextWithClient(context.Background(), makeOrgsRunner(mock))
	cmd.SetContext(ctx)

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, sub := range cmd.Commands() {
		sub.SilenceUsage = true
		sub.SilenceErrors = true
		for _, subsub := range sub.Commands() {
			subsub.SilenceUsage = true
			subsub.SilenceErrors = true
		}
	}

	err := cmd.Execute()

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	// stderr should contain the warnf warning.
	if !strings.Contains(stderr, "no fields specified for update") {
		t.Errorf("stderr = %q, want it to contain %q", stderr, "no fields specified for update")
	}

	// UpdateOrg should have been called with both fields nil.
	if !mock.updateOrgCalled {
		t.Fatal("UpdateOrg was not called; expected it to be called even with no flags")
	}
	if mock.updateOrgReq == nil {
		t.Fatal("UpdateOrg request is nil")
	}
	if mock.updateOrgReq.Name != nil {
		t.Errorf("req.Name = %v, want nil", mock.updateOrgReq.Name)
	}
	if mock.updateOrgReq.URL != nil {
		t.Errorf("req.URL = %v, want nil", mock.updateOrgReq.URL)
	}

	// stdout should be a JSON org object.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-44: akc admin orgs update is registered in the agent interface with
// method PATCH, path /orgs/:id, auth admin, positional arg id, optional
// flags name and url.
// Requirement: 14-REQ-14.3
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	updateCmd, _, err := cmd.Find([]string{"orgs", "update"})
	if err != nil {
		t.Fatalf("failed to find 'orgs update' command: %v", err)
	}
	if updateCmd.Name() != "update" {
		t.Fatalf("found command %q, want %q", updateCmd.Name(), "update")
	}

	annotations := updateCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs update command has no Annotations")
	}
	if annotations["method"] != "PATCH" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "PATCH")
	}
	if annotations["path"] != "/orgs/:id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}

	// Verify optional flags are registered (both optional for org update).
	nameFlag := updateCmd.Flags().Lookup("name")
	if nameFlag == nil {
		t.Error("--name flag not registered on orgs update command")
	}
	urlFlag := updateCmd.Flags().Lookup("url")
	if urlFlag == nil {
		t.Error("--url flag not registered on orgs update command")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E18: akc admin orgs update without the <id> positional argument
// exits with code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-14.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsUpdateMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("orgs", "update", "--name", "NewName")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ===========================================================================
// Task Group 3.3: admin orgs delete, block, unblock tests (REQ-15 – REQ-17)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-45: akc admin orgs delete <id> calls DeleteOrg and on success
// prints '{}' to stdout with exit code 0, no confirmation prompt.
// Requirement: 14-REQ-15.1
// ---------------------------------------------------------------------------

func TestAdminOrgsDeleteCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		deleteOrgErr: nil,
	}

	stdout, err := executeAdminCmd("orgs", "delete", "o1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.deleteOrgCalled {
		t.Fatal("DeleteOrg was not called")
	}
	if mock.deleteOrgID != "o1" {
		t.Errorf("captured ID = %q, want %q", mock.deleteOrgID, "o1")
	}

	// stdout should be exactly '{}'.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v (stdout=%q)", err, stdout)
	}
	if len(parsed) != 0 {
		t.Errorf("stdout = %q, want empty JSON object '{}'", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-46: akc admin orgs delete is registered in the agent interface with
// method DELETE, path /orgs/:id, auth admin.
// Requirement: 14-REQ-15.2
// ---------------------------------------------------------------------------

func TestAdminOrgsDeleteAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	deleteCmd, _, err := cmd.Find([]string{"orgs", "delete"})
	if err != nil {
		t.Fatalf("failed to find 'orgs delete' command: %v", err)
	}
	if deleteCmd.Name() != "delete" {
		t.Fatalf("found command %q, want %q", deleteCmd.Name(), "delete")
	}

	annotations := deleteCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs delete command has no Annotations")
	}
	if annotations["method"] != "DELETE" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "DELETE")
	}
	if annotations["path"] != "/orgs/:id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E19: akc admin orgs delete without the <id> positional argument
// exits with code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-15.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsDeleteMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("orgs", "delete")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-47: akc admin orgs block <id> calls BlockOrg and prints the
// returned org as JSON.
// Requirement: 14-REQ-16.1
// ---------------------------------------------------------------------------

func TestAdminOrgsBlockCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		blockOrgResult: &apikit.Organization{ID: "o1", Status: "blocked"},
	}

	stdout, err := executeAdminCmd("orgs", "block", "o1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.blockOrgCalled {
		t.Fatal("BlockOrg was not called")
	}
	if mock.blockOrgID != "o1" {
		t.Errorf("captured ID = %q, want %q", mock.blockOrgID, "o1")
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-48: akc admin orgs block is registered in the agent interface with
// method POST, path /orgs/:id/block, auth admin.
// Requirement: 14-REQ-16.2
// ---------------------------------------------------------------------------

func TestAdminOrgsBlockAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	blockCmd, _, err := cmd.Find([]string{"orgs", "block"})
	if err != nil {
		t.Fatalf("failed to find 'orgs block' command: %v", err)
	}
	if blockCmd.Name() != "block" {
		t.Fatalf("found command %q, want %q", blockCmd.Name(), "block")
	}

	annotations := blockCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs block command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/orgs/:id/block" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id/block")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E20: akc admin orgs block without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-16.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsBlockMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("orgs", "block")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-49: akc admin orgs unblock <id> calls UnblockOrg and prints the
// returned org as JSON.
// Requirement: 14-REQ-17.1
// ---------------------------------------------------------------------------

func TestAdminOrgsUnblockCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		unblockOrgResult: &apikit.Organization{ID: "o1", Status: "active"},
	}

	stdout, err := executeAdminCmd("orgs", "unblock", "o1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.unblockOrgCalled {
		t.Fatal("UnblockOrg was not called")
	}
	if mock.unblockOrgID != "o1" {
		t.Errorf("captured ID = %q, want %q", mock.unblockOrgID, "o1")
	}

	// stdout should be valid JSON.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-50: akc admin orgs unblock is registered in the agent interface
// with method POST, path /orgs/:id/unblock, auth admin.
// Requirement: 14-REQ-17.2
// ---------------------------------------------------------------------------

func TestAdminOrgsUnblockAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	unblockCmd, _, err := cmd.Find([]string{"orgs", "unblock"})
	if err != nil {
		t.Fatalf("failed to find 'orgs unblock' command: %v", err)
	}
	if unblockCmd.Name() != "unblock" {
		t.Fatalf("found command %q, want %q", unblockCmd.Name(), "unblock")
	}

	annotations := unblockCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs unblock command has no Annotations")
	}
	if annotations["method"] != "POST" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "POST")
	}
	if annotations["path"] != "/orgs/:id/unblock" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id/unblock")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E21: akc admin orgs unblock without the <id> argument exits with
// code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-17.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsUnblockMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("orgs", "unblock")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ===========================================================================
// Task Group 4.1: admin orgs members tests (REQ-18 – REQ-20)
// ===========================================================================

// ---------------------------------------------------------------------------
// TS-14-51: akc admin orgs members list <id> calls ListOrgMembers with the
// org id and prints the member array as JSON.
// Requirement: 14-REQ-18.1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersListCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		listMembersResult: []*apikit.User{{ID: "u1", Username: "alice"}},
	}

	stdout, err := executeAdminCmd("orgs", "members", "list", "o1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.listMembersCalled {
		t.Fatal("ListOrgMembers was not called")
	}
	if mock.listMembersOrgID != "o1" {
		t.Errorf("captured orgID = %q, want %q", mock.listMembersOrgID, "o1")
	}

	// stdout should be a JSON array with one user.
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("stdout is not valid JSON: %q", stdout)
	}

	var users []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &users); err != nil {
		t.Fatalf("failed to parse stdout as JSON array: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("got %d users, want 1", len(users))
	}
	if id, _ := users[0]["id"].(string); id != "u1" {
		t.Errorf("user[0].id = %q, want %q", id, "u1")
	}
}

// ---------------------------------------------------------------------------
// TS-14-E22: akc admin orgs members list without the <id> argument exits
// with code 2 and prints the missing-argument error envelope.
// Requirement: 14-REQ-18.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersListMissingID(t *testing.T) {
	stdout, err := executeAdminCmd("orgs", "members", "list")

	if err == nil {
		t.Error("expected error when <id> argument is missing")
	}

	if stdout == "" {
		t.Fatal("stdout is empty; expected JSON error envelope")
	}
	env := parseErrorEnvelope(t, stdout)
	if env.Error.Code != 0 {
		t.Errorf("error.code = %d, want 0", env.Error.Code)
	}
	if env.Error.Message != "missing required argument: id" {
		t.Errorf("error.message = %q, want %q", env.Error.Message, "missing required argument: id")
	}
}

// ---------------------------------------------------------------------------
// TS-14-52: akc admin orgs members list is registered in the agent interface
// with method GET, path /orgs/:id/members, auth admin.
// Requirement: 14-REQ-18.2
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersListAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	membersListCmd, _, err := cmd.Find([]string{"orgs", "members", "list"})
	if err != nil {
		t.Fatalf("failed to find 'orgs members list' command: %v", err)
	}
	if membersListCmd.Name() != "list" {
		t.Fatalf("found command %q, want %q", membersListCmd.Name(), "list")
	}

	annotations := membersListCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs members list command has no Annotations")
	}
	if annotations["method"] != "GET" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "GET")
	}
	if annotations["path"] != "/orgs/:id/members" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id/members")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-53: akc admin orgs members add <org_id> <user_id> calls AddOrgMember
// and prints '{}' on success.
// Requirement: 14-REQ-19.1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersAddCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		addMemberErr: nil,
	}

	stdout, err := executeAdminCmd("orgs", "members", "add", "o1", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.addMemberCalled {
		t.Fatal("AddOrgMember was not called")
	}
	if mock.addMemberOrgID != "o1" {
		t.Errorf("captured orgID = %q, want %q", mock.addMemberOrgID, "o1")
	}
	if mock.addMemberUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.addMemberUserID, "u1")
	}

	// stdout should be exactly '{}'.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v (stdout=%q)", err, stdout)
	}
	if len(parsed) != 0 {
		t.Errorf("stdout = %q, want empty JSON object '{}'", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-E23: akc admin orgs members add with missing positional arguments
// exits with code 2 and prints the appropriate missing-argument error.
// Requirement: 14-REQ-19.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersAddMissingArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "missing both org_id and user_id",
			args:    []string{"orgs", "members", "add"},
			wantMsg: "missing required argument: org_id",
		},
		{
			name:    "missing user_id",
			args:    []string{"orgs", "members", "add", "o1"},
			wantMsg: "missing required argument: user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAdminOrgsClient{}

			stdout, err := executeAdminCmd(tt.args...)

			if err == nil {
				t.Error("expected error when positional argument is missing")
			}

			if mock.addMemberCalled {
				t.Error("AddOrgMember was called despite missing argument")
			}

			if stdout == "" {
				t.Fatal("stdout is empty; expected JSON error envelope")
			}
			env := parseErrorEnvelope(t, stdout)
			if env.Error.Code != 0 {
				t.Errorf("error.code = %d, want 0", env.Error.Code)
			}
			if !strings.Contains(env.Error.Message, "missing required argument") {
				t.Errorf("error.message = %q, want it to contain %q", env.Error.Message, "missing required argument")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-14-54: akc admin orgs members add is registered in the agent interface
// with method PUT, path /orgs/:id/members/:user_id, auth admin, two
// positional args.
// Requirement: 14-REQ-19.2
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersAddAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	addCmd, _, err := cmd.Find([]string{"orgs", "members", "add"})
	if err != nil {
		t.Fatalf("failed to find 'orgs members add' command: %v", err)
	}
	if addCmd.Name() != "add" {
		t.Fatalf("found command %q, want %q", addCmd.Name(), "add")
	}

	annotations := addCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs members add command has no Annotations")
	}
	if annotations["method"] != "PUT" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "PUT")
	}
	if annotations["path"] != "/orgs/:id/members/:user_id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id/members/:user_id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// TS-14-55: akc admin orgs members remove <org_id> <user_id> calls
// RemoveOrgMember and prints '{}' on success.
// Requirement: 14-REQ-20.1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersRemoveCommand(t *testing.T) {
	mock := &mockAdminOrgsClient{
		removeMemberErr: nil,
	}

	stdout, err := executeAdminCmd("orgs", "members", "remove", "o1", "u1")

	if err != nil {
		t.Errorf("expected nil error (exit 0), got: %v", err)
	}

	if !mock.removeMemberCalled {
		t.Fatal("RemoveOrgMember was not called")
	}
	if mock.removeMemberOrgID != "o1" {
		t.Errorf("captured orgID = %q, want %q", mock.removeMemberOrgID, "o1")
	}
	if mock.removeMemberUserID != "u1" {
		t.Errorf("captured userID = %q, want %q", mock.removeMemberUserID, "u1")
	}

	// stdout should be exactly '{}'.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse stdout as JSON: %v (stdout=%q)", err, stdout)
	}
	if len(parsed) != 0 {
		t.Errorf("stdout = %q, want empty JSON object '{}'", stdout)
	}
}

// ---------------------------------------------------------------------------
// TS-14-E24: akc admin orgs members remove with missing positional arguments
// exits with code 2 and prints the appropriate missing-argument error.
// Requirement: 14-REQ-20.E1
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersRemoveMissingArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{
			name:    "missing both org_id and user_id",
			args:    []string{"orgs", "members", "remove"},
			wantMsg: "missing required argument: org_id",
		},
		{
			name:    "missing user_id",
			args:    []string{"orgs", "members", "remove", "o1"},
			wantMsg: "missing required argument: user_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAdminOrgsClient{}

			stdout, err := executeAdminCmd(tt.args...)

			if err == nil {
				t.Error("expected error when positional argument is missing")
			}

			if mock.removeMemberCalled {
				t.Error("RemoveOrgMember was called despite missing argument")
			}

			if stdout == "" {
				t.Fatal("stdout is empty; expected JSON error envelope")
			}
			env := parseErrorEnvelope(t, stdout)
			if env.Error.Code != 0 {
				t.Errorf("error.code = %d, want 0", env.Error.Code)
			}
			if !strings.Contains(env.Error.Message, "missing required argument") {
				t.Errorf("error.message = %q, want it to contain %q", env.Error.Message, "missing required argument")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TS-14-56: akc admin orgs members remove is registered in the agent
// interface with method DELETE, path /orgs/:id/members/:user_id, auth admin.
// Requirement: 14-REQ-20.2
// ---------------------------------------------------------------------------

func TestAdminOrgsMembersRemoveAgentInterface(t *testing.T) {
	cmd := cli.NewAdminCmd()

	removeCmd, _, err := cmd.Find([]string{"orgs", "members", "remove"})
	if err != nil {
		t.Fatalf("failed to find 'orgs members remove' command: %v", err)
	}
	if removeCmd.Name() != "remove" {
		t.Fatalf("found command %q, want %q", removeCmd.Name(), "remove")
	}

	annotations := removeCmd.Annotations
	if annotations == nil {
		t.Fatal("orgs members remove command has no Annotations")
	}
	if annotations["method"] != "DELETE" {
		t.Errorf("method annotation = %q, want %q", annotations["method"], "DELETE")
	}
	if annotations["path"] != "/orgs/:id/members/:user_id" {
		t.Errorf("path annotation = %q, want %q", annotations["path"], "/orgs/:id/members/:user_id")
	}
	if annotations["auth"] != "admin" {
		t.Errorf("auth annotation = %q, want %q", annotations["auth"], "admin")
	}
}

// ---------------------------------------------------------------------------
// Test helpers for mock injection
// ---------------------------------------------------------------------------

// makeOrgsRunner creates an OrgsRunner that wraps a mockAdminOrgsClient,
// bridging the typed mock interface to the any-typed function values used
// by the production code (which cannot import apikit due to import cycles).
func makeOrgsRunner(mock *mockAdminOrgsClient) *cli.OrgsRunner {
	return &cli.OrgsRunner{
		ListOrgs: func(ctx context.Context, includeBlocked bool) (any, error) {
			return mock.ListOrgs(ctx, &apikit.ListOrgsOptions{IncludeBlocked: includeBlocked})
		},
		CreateOrg: func(ctx context.Context, name, slug string, url *string) (any, error) {
			return mock.CreateOrg(ctx, &apikit.CreateOrgRequest{
				Name: name,
				Slug: slug,
				URL:  url,
			})
		},
		UpdateOrg: func(ctx context.Context, id string, name *string, url *string) (any, error) {
			return mock.UpdateOrg(ctx, id, &apikit.UpdateOrgRequest{
				Name: name,
				URL:  url,
			})
		},
		DeleteOrg: func(ctx context.Context, id string) error {
			return mock.DeleteOrg(ctx, id)
		},
		BlockOrg: func(ctx context.Context, id string) (any, error) {
			return mock.BlockOrg(ctx, id)
		},
		UnblockOrg: func(ctx context.Context, id string) (any, error) {
			return mock.UnblockOrg(ctx, id)
		},
		ListOrgMembers: func(ctx context.Context, orgID string) (any, error) {
			return mock.ListOrgMembers(ctx, orgID)
		},
		AddOrgMember: func(ctx context.Context, orgID, userID string) error {
			return mock.AddOrgMember(ctx, orgID, userID)
		},
		RemoveOrgMember: func(ctx context.Context, orgID, userID string) error {
			return mock.RemoveOrgMember(ctx, orgID, userID)
		},
	}
}

// Ensure imports are used.
var (
	_ adminOrgsClient = (*mockAdminOrgsClient)(nil)
	_ = strings.Contains
)
