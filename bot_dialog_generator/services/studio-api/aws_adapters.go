package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// ArtifactPublisher uploads compiled and KMS-signed bot definitions to S3 and optionally
// relays bot.published events to SQS (ADR 0001, ADR 0003, Milestone 2).
type ArtifactPublisher interface {
	PublishArtifact(ctx context.Context, workspaceID, botID, version string, artifact *botdef.CompiledArtifact) (*awsplatform.StoredDefinition, error)
}

// AWSArtifactPublisher implements ArtifactPublisher using awsplatform S3, KMS, and SQS.
type AWSArtifactPublisher struct {
	bucket         string
	s3             *awsplatform.S3Service
	sqs            *awsplatform.SQSService
	eventsQueueURL string
}

// NewAWSArtifactPublisher creates an AWSArtifactPublisher backed by S3, optional KMS signing,
// and optional SQS event notifications.
func NewAWSArtifactPublisher(bucket string, s3Svc *awsplatform.S3Service, sqsSvc *awsplatform.SQSService, eventsQueueURL string) *AWSArtifactPublisher {
	return &AWSArtifactPublisher{
		bucket:         bucket,
		s3:             s3Svc,
		sqs:            sqsSvc,
		eventsQueueURL: eventsQueueURL,
	}
}

// PublishArtifact stores the compiled Protobuf definition in S3 (signed with KMS if configured)
// and publishes a bot.published notification to SQS when an events queue is configured.
func (p *AWSArtifactPublisher) PublishArtifact(ctx context.Context, workspaceID, botID, version string, artifact *botdef.CompiledArtifact) (*awsplatform.StoredDefinition, error) {
	stored, err := p.s3.PutDefinition(ctx, p.bucket, workspaceID, botID, version, artifact.Bytes)
	if err != nil {
		return nil, err
	}
	if p.sqs != nil && p.eventsQueueURL != "" {
		_, err := p.sqs.SendJSON(ctx, p.eventsQueueURL, workspaceID, map[string]any{
			"event_type":   "bot.published",
			"workspace_id": workspaceID,
			"bot_id":       botID,
			"version":      version,
			"artifact_uri": stored.URI,
			"sha256":       stored.SHA256,
			"timestamp":    time.Now().UTC(),
		})
		if err != nil {
			return nil, fmt.Errorf("publish bot.published to SQS: %w", err)
		}
	}
	return stored, nil
}

// openArtifactPublisherFromEnv initializes an AWSArtifactPublisher when S3_DEFINITIONS_BUCKET
// is set in the environment (pointing at AWS Emulator or AWS).
func openArtifactPublisherFromEnv(ctx context.Context, getenv func(string) string) (ArtifactPublisher, error) {
	bucket := strings.TrimSpace(getenv("S3_DEFINITIONS_BUCKET"))
	if bucket == "" {
		return nil, nil
	}
	awsCfg, _, err := awsplatform.LoadConfig(ctx, getenv)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for S3_DEFINITIONS_BUCKET: %w", err)
	}
	var signer awsplatform.ArtifactSigner
	kmsKeyID := strings.TrimSpace(getenv("KMS_SIGNING_KEY_ID"))
	if kmsKeyID != "" {
		kmsSvc := awsplatform.NewKMSService(kms.NewFromConfig(awsCfg))
		resolvedKeyID, err := kmsSvc.EnsureSigningKey(ctx, kmsKeyID)
		if err != nil {
			return nil, fmt.Errorf("ensure KMS signing key %q: %w", kmsKeyID, err)
		}
		signer = kmsSvc
		kmsKeyID = resolvedKeyID
	}
	s3Svc := awsplatform.NewS3Service(awsplatform.NewS3Client(awsCfg), signer, kmsKeyID)
	if err := s3Svc.EnsureBucket(ctx, bucket, true); err != nil {
		return nil, fmt.Errorf("ensure S3 definitions bucket %q: %w", bucket, err)
	}

	var sqsSvc *awsplatform.SQSService
	var queueURL string
	if queueName := strings.TrimSpace(getenv("SQS_EVENTS_QUEUE")); queueName != "" {
		sqsSvc = awsplatform.NewSQSService(sqs.NewFromConfig(awsCfg))
		queueURL, err = sqsSvc.EnsureQueue(ctx, queueName)
		if err != nil {
			return nil, fmt.Errorf("ensure SQS events queue %q: %w", queueName, err)
		}
	}
	return NewAWSArtifactPublisher(bucket, s3Svc, sqsSvc, queueURL), nil
}

// resolveDatabaseURLFromSecret fetches database credentials from AWS Secrets Manager
// when DATABASE_URL is unset and DB_SECRET_ID is provided (migrating
// F:\artemis\back\src\config\secret-manager.ts).
func resolveDatabaseURLFromSecret(ctx context.Context, getenv func(string) string, secretsGetter func(context.Context, string) (awsplatform.DBSecret, error)) (string, error) {
	if dbURL := strings.TrimSpace(getenv("DATABASE_URL")); dbURL != "" {
		return dbURL, nil
	}
	secretID := strings.TrimSpace(getenv("DB_SECRET_ID"))
	if secretID == "" {
		return "", nil
	}
	if secretsGetter == nil {
		awsCfg, _, err := awsplatform.LoadConfig(ctx, getenv)
		if err != nil {
			return "", fmt.Errorf("load AWS config for DB_SECRET_ID: %w", err)
		}
		smSvc := awsplatform.NewSecretsService(secretsmanager.NewFromConfig(awsCfg))
		secretsGetter = smSvc.GetDBSecret
	}
	secret, err := secretsGetter(ctx, secretID)
	if err != nil {
		return "", fmt.Errorf("resolve DB_SECRET_ID %q: %w", secretID, err)
	}
	return secret.PostgresURL(), nil
}

// Ensure s3 package is referenced for type completeness.
var _ awsplatform.S3API = (*s3.Client)(nil)
