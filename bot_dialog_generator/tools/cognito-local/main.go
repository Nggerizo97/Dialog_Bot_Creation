// Command cognito-local creates the studio's Amazon Cognito setup in a local AWS
// Emulator: a user pool, the studio's app client, the groups that grant access, and
// the six demo users from the README, so the studio can sign in with Cognito instead
// of the development sign-in.
//
// It only talks to an emulator on this machine. The emulator keeps everything in
// memory, so run this again each time the emulator starts. See docs/cognito-setup.md.
//
//	go run ./tools/cognito-local                  # print the settings
//	go run ./tools/cognito-local -write-env       # also write apps/studio-web/.env.cognito.local
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

// DemoUser mirrors the demo users of the development sign-in (apps/studio-web/src/Login.tsx)
// and the memberships seeded in services/studio-api/seed.go.
type DemoUser struct {
	Username string
	Groups   []string
}

var DemoUsers = []DemoUser{
	{Username: "alice"},
	{Username: "erin"},
	{Username: "carol"},
	{Username: "bob"},
	{Username: "frank", Groups: []string{"hr-team"}},
	{Username: "dana", Groups: []string{"bdg-platform-admins"}},
}

// DemoGroups are the Cognito groups the studio's access rules refer to.
var DemoGroups = []string{"bdg-platform-admins", "hr-team"}

// Options control what Bootstrap creates.
type Options struct {
	PoolName     string
	ClientName   string
	Password     string
	CallbackURLs []string
}

// Result is what the studio needs to sign in against the pool.
type Result struct {
	UserPoolID string
	ClientID   string
	// Issuer is read from a real sign-in, so it is exactly what tokens carry.
	Issuer string
}

func main() {
	opts := Options{ClientName: "bdg-studio", CallbackURLs: []string{"http://localhost:5173/"}}
	flag.StringVar(&opts.PoolName, "pool", "bdg-local", "user pool name")
	flag.StringVar(&opts.Password, "password", envOr("COGNITO_DEMO_PASSWORD", "Demo-pass-2026"), "password for every demo user (local emulator only)")
	writeEnv := flag.Bool("write-env", false, "write the studio's settings to apps/studio-web/.env.cognito.local")
	flag.Parse()
	log.SetFlags(0)

	ctx := context.Background()
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if err := RequireLocalEndpoint(endpoint); err != nil {
		log.Fatal(err)
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(envOr("AWS_REGION", "us-east-1")))
	if err != nil {
		log.Fatalf("load AWS settings: %v", err)
	}
	client := cip.NewFromConfig(cfg)

	result, err := Bootstrap(ctx, client, opts)
	if err != nil {
		log.Fatal(explain(err))
	}

	caFile := os.Getenv("AWS_CA_BUNDLE")
	fmt.Printf("Created user pool %s with app client %s.\n", result.UserPoolID, result.ClientID)
	fmt.Printf("Demo users: %s. Password: %s (local emulator only).\n\n", usernames(), opts.Password)
	fmt.Println("studio-api, bash:")
	for _, kv := range studioAPIEnv(result, caFile) {
		fmt.Printf("  export %s=%q\n", kv[0], kv[1])
	}
	fmt.Println("\nstudio-api, PowerShell:")
	for _, kv := range studioAPIEnv(result, caFile) {
		fmt.Printf("  $env:%s = %q\n", kv[0], kv[1])
	}
	fmt.Println("\nstudio-web (apps/studio-web/.env.cognito.local), then: npm --prefix apps/studio-web run dev:cognito")
	webEnv := studioWebEnv(result)
	fmt.Print(indent(webEnv))
	if *writeEnv {
		path := filepath.Join("apps", "studio-web", ".env.cognito.local")
		if err := os.WriteFile(path, []byte(webEnv), 0o600); err != nil {
			log.Fatalf("write %s: %v", path, err)
		}
		fmt.Printf("\nWrote %s.\n", path)
	}
}

// RequireLocalEndpoint refuses anything but an emulator on this machine: the demo users
// have a known password and must never be created in a real AWS account.
func RequireLocalEndpoint(endpoint string) error {
	if endpoint == "" {
		return errors.New("AWS_ENDPOINT_URL is not set. Point it at the AWS Emulator (https://127.0.0.1:9297); this tool never runs against AWS")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("AWS_ENDPOINT_URL %q is not a URL", endpoint)
	}
	if ip := net.ParseIP(u.Hostname()); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("AWS_ENDPOINT_URL %q is not on this machine; this tool only runs against a local AWS Emulator (use 127.0.0.1)", endpoint)
	}
	return nil
}

// Bootstrap creates (or finds) the pool, app client, groups and demo users, then signs
// in once to prove the setup works and to learn the issuer.
func Bootstrap(ctx context.Context, c *cip.Client, opts Options) (*Result, error) {
	poolID, err := ensurePool(ctx, c, opts.PoolName)
	if err != nil {
		return nil, fmt.Errorf("user pool: %w", err)
	}
	clientID, err := ensureClient(ctx, c, poolID, opts)
	if err != nil {
		return nil, fmt.Errorf("app client: %w", err)
	}
	for _, g := range DemoGroups {
		_, err := c.CreateGroup(ctx, &cip.CreateGroupInput{UserPoolId: &poolID, GroupName: aws.String(g)})
		if err != nil && !isAlreadyThere(err) {
			return nil, fmt.Errorf("group %s: %w", g, err)
		}
	}
	for _, u := range DemoUsers {
		if err := ensureUser(ctx, c, poolID, u, opts.Password); err != nil {
			return nil, fmt.Errorf("user %s: %w", u.Username, err)
		}
	}
	out, err := c.InitiateAuth(ctx, &cip.InitiateAuthInput{
		ClientId: &clientID, AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		AuthParameters: map[string]string{"USERNAME": DemoUsers[0].Username, "PASSWORD": opts.Password},
	})
	if err != nil {
		return nil, fmt.Errorf("test sign-in as %s: %w", DemoUsers[0].Username, err)
	}
	if out.AuthenticationResult == nil || out.AuthenticationResult.AccessToken == nil {
		return nil, fmt.Errorf("test sign-in as %s returned no tokens (challenge %s)", DemoUsers[0].Username, out.ChallengeName)
	}
	issuer, err := issuerOf(*out.AuthenticationResult.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Result{UserPoolID: poolID, ClientID: clientID, Issuer: issuer}, nil
}

func ensurePool(ctx context.Context, c *cip.Client, name string) (string, error) {
	pages := cip.NewListUserPoolsPaginator(c, &cip.ListUserPoolsInput{MaxResults: aws.Int32(60)})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", err
		}
		for _, p := range page.UserPools {
			if aws.ToString(p.Name) == name {
				return aws.ToString(p.Id), nil
			}
		}
	}
	out, err := c.CreateUserPool(ctx, &cip.CreateUserPoolInput{
		PoolName: &name,
		Policies: &types.UserPoolPolicyType{PasswordPolicy: &types.PasswordPolicyType{
			MinimumLength: aws.Int32(12), RequireLowercase: true, RequireUppercase: true, RequireNumbers: true,
		}},
		// Only staff accounts that IT creates; nobody signs themselves up.
		AdminCreateUserConfig: &types.AdminCreateUserConfigType{AllowAdminCreateUserOnly: true},
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UserPool.Id), nil
}

func ensureClient(ctx context.Context, c *cip.Client, poolID string, opts Options) (string, error) {
	list, err := c.ListUserPoolClients(ctx, &cip.ListUserPoolClientsInput{UserPoolId: &poolID, MaxResults: aws.Int32(60)})
	if err != nil {
		return "", err
	}
	for _, cl := range list.UserPoolClients {
		if aws.ToString(cl.ClientName) == opts.ClientName {
			return aws.ToString(cl.ClientId), nil
		}
	}
	out, err := c.CreateUserPoolClient(ctx, &cip.CreateUserPoolClientInput{
		UserPoolId: &poolID, ClientName: &opts.ClientName,
		// A browser app cannot keep a secret.
		GenerateSecret: false,
		// USER_PASSWORD_AUTH is for the local emulator, which has no hosted sign-in page.
		// A production client uses managed login (code flow with PKCE) instead.
		ExplicitAuthFlows: []types.ExplicitAuthFlowsType{
			types.ExplicitAuthFlowsTypeAllowUserPasswordAuth, types.ExplicitAuthFlowsTypeAllowUserSrpAuth, types.ExplicitAuthFlowsTypeAllowRefreshTokenAuth,
		},
		AllowedOAuthFlows:               []types.OAuthFlowType{types.OAuthFlowTypeCode},
		AllowedOAuthFlowsUserPoolClient: true,
		AllowedOAuthScopes:              []string{"openid", "email", "profile"},
		CallbackURLs:                    opts.CallbackURLs,
		LogoutURLs:                      opts.CallbackURLs,
		SupportedIdentityProviders:      []string{"COGNITO"},
		PreventUserExistenceErrors:      types.PreventUserExistenceErrorTypesEnabled,
		AccessTokenValidity:             aws.Int32(60),
		IdTokenValidity:                 aws.Int32(60),
		TokenValidityUnits:              &types.TokenValidityUnitsType{AccessToken: types.TimeUnitsTypeMinutes, IdToken: types.TimeUnitsTypeMinutes},
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UserPoolClient.ClientId), nil
}

func ensureUser(ctx context.Context, c *cip.Client, poolID string, u DemoUser, password string) error {
	_, err := c.AdminCreateUser(ctx, &cip.AdminCreateUserInput{
		UserPoolId: &poolID, Username: &u.Username, MessageAction: types.MessageActionTypeSuppress,
		UserAttributes: []types.AttributeType{
			// Reserved example domain: no real mailbox receives anything.
			{Name: aws.String("email"), Value: aws.String(u.Username + "@example.com")},
			{Name: aws.String("email_verified"), Value: aws.String("true")},
		},
	})
	if err != nil && !isAlreadyThere(err) {
		return err
	}
	if _, err := c.AdminSetUserPassword(ctx, &cip.AdminSetUserPasswordInput{
		UserPoolId: &poolID, Username: &u.Username, Password: &password, Permanent: true,
	}); err != nil {
		return err
	}
	for _, g := range u.Groups {
		if _, err := c.AdminAddUserToGroup(ctx, &cip.AdminAddUserToGroupInput{UserPoolId: &poolID, Username: &u.Username, GroupName: aws.String(g)}); err != nil {
			return err
		}
	}
	return nil
}

func isAlreadyThere(err error) bool {
	var exists *types.UsernameExistsException
	var group *types.GroupExistsException
	return errors.As(err, &exists) || errors.As(err, &group)
}

func studioAPIEnv(r *Result, caFile string) [][2]string {
	env := [][2]string{
		{"AUTH_MODE", "cognito"},
		{"COGNITO_USER_POOL_ID", r.UserPoolID},
		{"COGNITO_ISSUER", r.Issuer},
		{"COGNITO_CLIENT_IDS", r.ClientID},
		// The seeded memberships name people by their sign-in name (alice, bob, ...).
		{"OIDC_SUBJECT_CLAIM", "username"},
	}
	if caFile != "" {
		env = append(env, [2]string{"OIDC_CA_FILE", caFile})
	}
	return env
}

func studioWebEnv(r *Result) string {
	return "# Written by tools/cognito-local. The emulator forgets this pool when it stops: run the tool again.\n" +
		"VITE_COGNITO_CLIENT_ID=" + r.ClientID + "\n" +
		"# The Vite dev server forwards this path to the AWS Emulator (see vite.config.ts).\n" +
		"VITE_COGNITO_ENDPOINT=/__cognito\n"
}

func usernames() string {
	names := make([]string, len(DemoUsers))
	for i, u := range DemoUsers {
		names[i] = u.Username
	}
	return strings.Join(names, ", ")
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ") + "\n"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// explain adds the fix for the errors people hit first, from the AWS Emulator guide.
func explain(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Sign the request with the emulator credentials"):
		return msg + "\nSet AWS_SHARED_CREDENTIALS_FILE and AWS_CONFIG_FILE to the emulator's files, and unset AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN and AWS_PROFILE (docs/cognito-setup.md)."
	case strings.Contains(msg, "certificate"):
		return msg + "\nSet AWS_CA_BUNDLE to the emulator's ca.pem, for example ~/.aws/emulator/9297/ca.pem."
	case strings.Contains(msg, "connection refused"):
		return msg + "\nStart the AWS Emulator first (docs/cognito-setup.md)."
	}
	return msg
}
