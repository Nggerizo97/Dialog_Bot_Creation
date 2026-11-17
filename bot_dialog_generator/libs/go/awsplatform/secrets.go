package awsplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/smithy-go"
)

// SecretsManagerAPI is the subset of the AWS Secrets Manager client used by SecretsService.
type SecretsManagerAPI interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	CreateSecret(ctx context.Context, params *secretsmanager.CreateSecretInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
	PutSecretValue(ctx context.Context, params *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
}

// DBSecret represents the JSON database credentials structure migrated from
// F:\artemis\back\src\config\secret-manager.ts (getDBSecrets).
type DBSecret struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode,omitempty"`
}

// PostgresURL formats DBSecret into a PostgreSQL connection URL.
func (d DBSecret) PostgresURL() string {
	host := d.Host
	if host == "" {
		host = "localhost"
	}
	port := d.Port
	if port == 0 {
		port = 5432
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.Username, d.Password),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   d.DBName,
	}
	if d.SSLMode != "" {
		q := u.Query()
		q.Set("sslmode", d.SSLMode)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// SecretsService migrates F:\artemis\back\src\config\secret-manager.ts to Go.
type SecretsService struct {
	client SecretsManagerAPI
}

// NewSecretsService wraps an AWS Secrets Manager client.
func NewSecretsService(client SecretsManagerAPI) *SecretsService {
	return &SecretsService{client: client}
}

// GetSecretJSON fetches a secret by ID and unmarshals its SecretString JSON into target
// (migrating getDBSecrets in F:\artemis\back\src\config\secret-manager.ts).
func (s *SecretsService) GetSecretJSON(ctx context.Context, secretID string, target any) error {
	if strings.TrimSpace(secretID) == "" {
		return errors.New("awsplatform: secretID is required")
	}
	out, err := s.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return fmt.Errorf("awsplatform: get secret %q: %w", secretID, err)
	}
	raw := aws.ToString(out.SecretString)
	if raw == "" && len(out.SecretBinary) > 0 {
		raw = string(out.SecretBinary)
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("awsplatform: decode secret %q JSON: %w", secretID, err)
	}
	return nil
}

// GetDBSecret fetches and parses database credentials from AWS Secrets Manager.
func (s *SecretsService) GetDBSecret(ctx context.Context, secretID string) (DBSecret, error) {
	var secret DBSecret
	if err := s.GetSecretJSON(ctx, secretID, &secret); err != nil {
		return DBSecret{}, err
	}
	return secret, nil
}

// PutSecretJSON creates or updates a JSON secret in AWS Secrets Manager / AWS Emulator.
func (s *SecretsService) PutSecretJSON(ctx context.Context, secretID string, value any) error {
	if strings.TrimSpace(secretID) == "" {
		return errors.New("awsplatform: secretID is required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("awsplatform: marshal secret %q: %w", secretID, err)
	}
	_, err = s.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
		Name:         aws.String(secretID),
		SecretString: aws.String(string(data)),
	})
	if err != nil {
		if isSecretExists(err) {
			_, putErr := s.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
				SecretId:     aws.String(secretID),
				SecretString: aws.String(string(data)),
			})
			if putErr != nil {
				return fmt.Errorf("awsplatform: update secret %q: %w", secretID, putErr)
			}
			return nil
		}
		return fmt.Errorf("awsplatform: create secret %q: %w", secretID, err)
	}
	return nil
}

func isSecretExists(err error) bool {
	var exists *smtypes.ResourceExistsException
	if errors.As(err, &exists) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "ResourceExistsException" {
		return true
	}
	return false
}
