// Package awsplatform provides AWS SDK v2 clients and migrated Artemis AWS
// services (S3, KMS, SQS, Secrets Manager) configured for both AWS Emulator
// (https://127.0.0.1:9297) and production AWS.
package awsplatform

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	// DefaultEmulatorPort is the default HTTPS port served by AWS Emulator.
	DefaultEmulatorPort = "9297"
	// DefaultEmulatorEndpoint is the default local endpoint for AWS Emulator.
	DefaultEmulatorEndpoint = "https://127.0.0.1:9297"
	// DefaultRegion is the single region recommended by the AWS Emulator User Guide.
	DefaultRegion = "us-east-1"
	// EmulatorAccountID is the fixed AWS account ID returned by AWS Emulator.
	EmulatorAccountID = "000000000000"
	// EmulatorUserARN is the caller identity ARN returned by AWS Emulator.
	EmulatorUserARN = "arn:aws:iam::000000000000:user/emulator"
)

// Options holds resolved settings for connecting to AWS or AWS Emulator.
type Options struct {
	Region                     string
	EndpointURL                string
	SharedCredentialsFile      string
	SharedConfigFile           string
	CABundleFile               string
	DisableHostPrefixInjection bool
	EC2MetadataDisabled        bool
	HTTPClient                 *http.Client
}

// StartupInfo represents the structured JSON startup log line emitted by aws-emulator.
// The AWS Emulator User Guide guarantees the "emulator server information" message
// and its fields remain compatible within a major version.
type StartupInfo struct {
	Message            string `json:"message"`
	EndpointURL        string `json:"endpoint_url"`
	HealthCheckURL     string `json:"health_check_url"`
	ConfigFile         string `json:"config_file"`
	CAFile             string `json:"ca_file"`
	JavaTruststoreFile string `json:"java_truststore_file"`
}

// ParseStartupLine scans JSON lines from aws-emulator standard output and returns
// the startup line whose message field is "emulator server information".
func ParseStartupLine(r io.Reader) (*StartupInfo, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var info StartupInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			continue
		}
		if info.Message == "emulator server information" && info.EndpointURL != "" {
			return &info, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("awsplatform: emulator startup line not found")
}

// ResolveOptions reads environment variables following the AWS Emulator User Guide.
// When AWS_ENDPOINT_URL points at a loopback emulator (e.g. https://127.0.0.1:9297),
// it automatically fills default paths under ~/.aws/emulator/<port>/ if not explicitly set.
func ResolveOptions(getenv func(string) string) (Options, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	region := strings.TrimSpace(getenv("AWS_REGION"))
	if region == "" {
		region = strings.TrimSpace(getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		region = DefaultRegion
	}

	endpointURL := strings.TrimSpace(getenv("AWS_ENDPOINT_URL"))
	credsFile := expandHome(strings.TrimSpace(getenv("AWS_SHARED_CREDENTIALS_FILE")), getenv)
	cfgFile := expandHome(strings.TrimSpace(getenv("AWS_CONFIG_FILE")), getenv)
	caFile := expandHome(strings.TrimSpace(getenv("AWS_CA_BUNDLE")), getenv)
	if caFile == "" {
		caFile = expandHome(strings.TrimSpace(getenv("SSL_CERT_FILE")), getenv)
	}

	disablePrefix := strings.EqualFold(strings.TrimSpace(getenv("AWS_DISABLE_HOST_PREFIX_INJECTION")), "true")
	ec2Disabled := strings.EqualFold(strings.TrimSpace(getenv("AWS_EC2_METADATA_DISABLED")), "true")

	if isEmulatorURL(endpointURL) {
		disablePrefix = true
		ec2Disabled = true
		port := extractPort(endpointURL)
		if home := userHome(getenv); home != "" {
			portDir := filepath.Join(home, ".aws", "emulator", port)
			if credsFile == "" {
				candidate := filepath.Join(portDir, "emulator_credentials")
				if fileExists(candidate) {
					credsFile = candidate
				}
			}
			if caFile == "" {
				candidate := filepath.Join(portDir, "ca.pem")
				if fileExists(candidate) {
					caFile = candidate
				}
			}
			if cfgFile == "" {
				candidate := filepath.Join(portDir, "config")
				if fileExists(candidate) {
					cfgFile = candidate
				}
			}
		}
	}

	httpClient, err := buildHTTPClient(caFile)
	if err != nil {
		return Options{}, err
	}

	return Options{
		Region:                     region,
		EndpointURL:                endpointURL,
		SharedCredentialsFile:      credsFile,
		SharedConfigFile:           cfgFile,
		CABundleFile:               caFile,
		DisableHostPrefixInjection: disablePrefix,
		EC2MetadataDisabled:        ec2Disabled,
		HTTPClient:                 httpClient,
	}, nil
}

// LoadConfig builds an aws.Config using ResolveOptions.
// On Windows with Docker, the config file written inside the container references
// /home/emulator/.aws/emulator/<port>/ca.pem; LoadConfig trusts ca.pem directly via
// the HTTP client TLS RootCAs pool so container-internal paths never break host SDKs.
func LoadConfig(ctx context.Context, getenv func(string) string) (aws.Config, Options, error) {
	opts, err := ResolveOptions(getenv)
	if err != nil {
		return aws.Config{}, Options{}, err
	}
	cfg, err := LoadConfigWithOptions(ctx, opts)
	if err != nil {
		return aws.Config{}, Options{}, err
	}
	return cfg, opts, nil
}

// LoadConfigWithOptions builds an aws.Config from an explicit Options struct.
func LoadConfigWithOptions(ctx context.Context, opts Options) (aws.Config, error) {
	if opts.Region == "" {
		opts.Region = DefaultRegion
	}
	if opts.HTTPClient == nil {
		hc, err := buildHTTPClient(opts.CABundleFile)
		if err != nil {
			return aws.Config{}, err
		}
		opts.HTTPClient = hc
	}

	var loadOpts []func(*config.LoadOptions) error
	loadOpts = append(loadOpts,
		config.WithRegion(opts.Region),
		config.WithHTTPClient(opts.HTTPClient),
	)
	if opts.EndpointURL != "" {
		loadOpts = append(loadOpts, config.WithBaseEndpoint(opts.EndpointURL))
	}
	if opts.DisableHostPrefixInjection {
		loadOpts = append(loadOpts, config.WithAPIOptions([]func(*middleware.Stack) error{
			func(stack *middleware.Stack) error {
				return stack.Initialize.Add(middleware.InitializeMiddlewareFunc("DisableHostPrefixInjection", func(
					ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler,
				) (middleware.InitializeOutput, middleware.Metadata, error) {
					return next.HandleInitialize(smithyhttp.DisableEndpointHostPrefix(ctx, true), in)
				}), middleware.Before)
			},
		}))
	}
	if opts.EC2MetadataDisabled {
		loadOpts = append(loadOpts, config.WithEC2IMDSClientEnableState(imds.ClientDisabled))
	}
	if opts.SharedCredentialsFile != "" {
		loadOpts = append(loadOpts, config.WithSharedCredentialsFiles([]string{opts.SharedCredentialsFile}))
		// Avoid loading a Docker-generated shared config file on Windows if it contains
		// container-internal /home/emulator paths, because EndpointURL, Region, and
		// CABundleFile are already applied directly.
		if opts.SharedConfigFile != "" && !configHasContainerPath(opts.SharedConfigFile) {
			loadOpts = append(loadOpts, config.WithSharedConfigFiles([]string{opts.SharedConfigFile}))
		} else {
			loadOpts = append(loadOpts, config.WithSharedConfigFiles([]string{}))
		}
	} else if opts.SharedConfigFile != "" && !configHasContainerPath(opts.SharedConfigFile) {
		loadOpts = append(loadOpts, config.WithSharedConfigFiles([]string{opts.SharedConfigFile}))
	}

	return config.LoadDefaultConfig(ctx, loadOpts...)
}

// CheckHealth queries the AWS Emulator control route GET /__admin/health and verifies
// that it returns HTTP 200 with body "ok".
func CheckHealth(ctx context.Context, endpointURL, caFile string) error {
	if endpointURL == "" {
		endpointURL = DefaultEmulatorEndpoint
	}
	hc, err := buildHTTPClient(caFile)
	if err != nil {
		return err
	}
	healthURL := strings.TrimRight(endpointURL, "/") + "/__admin/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("awsplatform: build health request: %w", err)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("awsplatform: health check failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("awsplatform: unexpected health response: status=%d body=%q", resp.StatusCode, string(body))
	}
	return nil
}

// VerifyEmulatorIdentity calls STS GetCallerIdentity and confirms that requests are
// reaching AWS Emulator (Account 000000000000, ARN arn:aws:iam::000000000000:user/emulator)
// rather than real AWS.
func VerifyEmulatorIdentity(ctx context.Context, cfg aws.Config) (*sts.GetCallerIdentityOutput, error) {
	client := sts.NewFromConfig(cfg)
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: sts get-caller-identity: %w", err)
	}
	if aws.ToString(out.Account) != EmulatorAccountID {
		return out, fmt.Errorf("awsplatform: connected account is %q, want emulator account %q (requests may be going to real AWS)", aws.ToString(out.Account), EmulatorAccountID)
	}
	return out, nil
}

// buildHTTPClient returns an *http.Client that trusts both the system root CAs and
// the AWS Emulator certificate (ca.pem) if provided, allowing the same process to
// reach both the local emulator and external HTTPS servers (User Guide p. 24).
func buildHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	if caFile == "" {
		return &http.Client{Transport: transport, Timeout: 60 * time.Second}, nil
	}
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("awsplatform: read CA bundle %q: %w", caFile, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("awsplatform: no valid PEM certificates found in %q", caFile)
	}
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}, nil
}

func isEmulatorURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost")
}

func extractPort(raw string) string {
	u, err := url.Parse(raw)
	if err == nil {
		if _, p, err := net.SplitHostPort(u.Host); err == nil && p != "" {
			return p
		}
	}
	return DefaultEmulatorPort
}

func userHome(getenv func(string) string) string {
	if h := strings.TrimSpace(getenv("HOME")); h != "" {
		return h
	}
	if h := strings.TrimSpace(getenv("USERPROFILE")); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func expandHome(p string, getenv func(string) string) string {
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home := userHome(getenv); home != "" {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func configHasContainerPath(cfgFile string) bool {
	b, err := os.ReadFile(cfgFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "/home/emulator/") && !fileExists("/home/emulator/.aws/emulator")
}
