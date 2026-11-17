package main

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

func TestOnlyRunsAgainstAnEmulatorOnThisMachine(t *testing.T) {
	for _, ok := range []string{"https://127.0.0.1:9297", "https://127.0.0.1:9300/", "https://[::1]:9297"} {
		if err := RequireLocalEndpoint(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "https://cognito-idp.us-east-1.amazonaws.com", "https://10.0.0.5:9297", "https://localhost.example.com:9297", "not a url"} {
		if err := RequireLocalEndpoint(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestMissingCertificateMeansTheEmulatorIsNotRunning(t *testing.T) {
	err := RequireCertificate("/nonexistent/.aws/emulator/9297/ca.pem")
	if err == nil || !strings.Contains(err.Error(), "the AWS Emulator is not running") || !strings.Contains(err.Error(), "aws-emulator --telemetry off") {
		t.Errorf("err = %v", err)
	}
	if err := RequireCertificate(""); err != nil {
		t.Errorf("no AWS_CA_BUNDLE: %v", err)
	}
	if err := RequireCertificate("main.go"); err != nil {
		t.Errorf("existing file: %v", err)
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"cognito":                       "'cognito'",
		"/home/ana/My Files/ca.pem":     "'/home/ana/My Files/ca.pem'",
		"it's":                          `'it'\''s'`,
		"https://127.0.0.1:9297/pool_1": "'https://127.0.0.1:9297/pool_1'",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestIssuerOf(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://127.0.0.1:9297/us-east-1_abc"}`))
	if got, err := issuerOf("h." + payload + ".s"); err != nil || got != "https://127.0.0.1:9297/us-east-1_abc" {
		t.Errorf("issuerOf = %q, %v", got, err)
	}
	for _, bad := range []string{"", "a.b", "h." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".s"} {
		if _, err := issuerOf(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestDemoUsersMatchTheSeededStudio(t *testing.T) {
	want := []string{"alice", "erin", "carol", "bob", "frank", "dana"}
	if got := usernames(); got != "alice, erin, carol, bob, frank, dana" {
		t.Errorf("usernames = %s, want %v", got, want)
	}
}

// TestAgainstTheAWSEmulator runs the whole local setup against a running AWS Emulator
// and checks every demo user's token with the verifier studio-api uses. It needs the
// emulator settings from docs/cognito-setup.md and COGNITO_EMULATOR_TEST=1.
func TestAgainstTheAWSEmulator(t *testing.T) {
	if os.Getenv("COGNITO_EMULATOR_TEST") != "1" {
		t.Skip("set COGNITO_EMULATOR_TEST=1 with a running AWS Emulator to run this test")
	}
	if err := RequireLocalEndpoint(os.Getenv("AWS_ENDPOINT_URL")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		t.Fatal(err)
	}
	client := cip.NewFromConfig(cfg)
	opts := Options{PoolName: "bdg-test-" + t.Name(), ClientName: "bdg-studio", Password: "Test-pass-2026", CallbackURLs: []string{"http://localhost:5173/"}}

	first, err := Bootstrap(ctx, client, opts)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// Running it again reuses what exists instead of failing or duplicating.
	again, err := Bootstrap(ctx, client, opts)
	if err != nil || *again != *first {
		t.Fatalf("second run: %+v, %v; want %+v", again, err, first)
	}

	httpClient, err := auth.HTTPClientWithCA(os.Getenv("AWS_CA_BUNDLE"))
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := auth.NewCognitoVerifier(ctx, auth.CognitoConfig{
		Issuer: first.Issuer, ClientIDs: []string{first.ClientID}, SubjectClaim: "username", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatalf("NewCognitoVerifier against the emulator: %v", err)
	}

	for _, user := range DemoUsers {
		out, err := client.InitiateAuth(ctx, &cip.InitiateAuthInput{
			ClientId: &first.ClientID, AuthFlow: types.AuthFlowTypeUserPasswordAuth,
			AuthParameters: map[string]string{"USERNAME": user.Username, "PASSWORD": opts.Password},
		})
		if err != nil {
			t.Fatalf("%s: sign in: %v", user.Username, err)
		}
		claims, err := verifier.Verify(ctx, aws.ToString(out.AuthenticationResult.AccessToken))
		if err != nil {
			t.Fatalf("%s: verify access token: %v", user.Username, err)
		}
		if claims.Subject != user.Username || !slices.Equal(claims.Groups, user.Groups) {
			t.Errorf("%s: claims = %+v, want groups %v", user.Username, claims, user.Groups)
		}
		if _, err := verifier.Verify(ctx, aws.ToString(out.AuthenticationResult.IdToken)); !errors.Is(err, auth.ErrWrongTokenUse) {
			t.Errorf("%s: ID token accepted as an access token: %v", user.Username, err)
		}
	}

	// A wrong password gets the same answer as on AWS.
	_, err = client.InitiateAuth(ctx, &cip.InitiateAuthInput{
		ClientId: &first.ClientID, AuthFlow: types.AuthFlowTypeUserPasswordAuth,
		AuthParameters: map[string]string{"USERNAME": "alice", "PASSWORD": "wrong-Password-1"},
	})
	var notAuthorized *types.NotAuthorizedException
	if !errors.As(err, &notAuthorized) {
		t.Errorf("wrong password: err = %v, want NotAuthorizedException", err)
	}
}
