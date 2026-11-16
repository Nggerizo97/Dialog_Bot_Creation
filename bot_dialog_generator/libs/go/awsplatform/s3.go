package awsplatform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	// MaxMultimediaTotalSizeBytes is the default 50 MiB total media cap migrated
	// from F:\artemis\back\src\modules\application\infra\services\get-multimedia.service.ts.
	MaxMultimediaTotalSizeBytes int64 = 50 * 1024 * 1024
)

var (
	// ErrInvalidScope is returned when a workspace, bot, or key segment is missing or unsafe.
	ErrInvalidScope = errors.New("awsplatform: invalid workspace or resource scope")
	// ErrObjectNotFound is returned when a requested S3 object does not exist.
	ErrObjectNotFound = errors.New("awsplatform: object not found")
	// ErrSignatureInvalid is returned when a stored bot definition fails integrity or KMS verification.
	ErrSignatureInvalid = errors.New("awsplatform: artifact signature or digest mismatch")
)

// ArtifactSigner signs and verifies compiled bot definitions (implemented by KMSService).
type ArtifactSigner interface {
	SignArtifact(ctx context.Context, keyID string, payload []byte) ([]byte, error)
	VerifyArtifact(ctx context.Context, keyID string, payload, signature []byte) error
}

// S3API is the subset of the AWS S3 client used by S3Service.
type S3API interface {
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	CopyObject(ctx context.Context, params *s3.CopyObjectInput, optFns ...func(*s3.Options)) (*s3.CopyObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	ListObjectVersions(ctx context.Context, params *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
}

// S3Service migrates the S3 capabilities from F:\artemis (back, bot_engine, media_admin,
// ingester_lambda, nodes_validator_lambda, pickle_rollback_lambda, pipeline_trigger_lambda)
// while enforcing Bot_Dialog_Generator's workspace isolation (ADR 0003).
type S3Service struct {
	client   S3API
	signer   ArtifactSigner
	kmsKeyID string
}

// NewS3Client creates an AWS SDK v2 S3 client configured with path-style addressing,
// as required by AWS Emulator (User Guide p. 21: "The emulator doesn't accept requests
// that put the bucket name in the host name.").
func NewS3Client(cfg aws.Config) *s3.Client {
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})
}

// NewS3Service wraps an S3API and optional KMS ArtifactSigner.
func NewS3Service(client S3API, signer ArtifactSigner, kmsKeyID string) *S3Service {
	return &S3Service{
		client:   client,
		signer:   signer,
		kmsKeyID: kmsKeyID,
	}
}

// EnsureBucket creates the bucket if it does not exist and optionally enables versioning
// (used by pickle_rollback_lambda / definition rollback).
func (s *S3Service) EnsureBucket(ctx context.Context, bucket string, versioned bool) error {
	if err := validateSegment(bucket); err != nil {
		return err
	}
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err != nil {
		_, createErr := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
		if createErr != nil && !isBucketAlreadyExists(createErr) {
			return fmt.Errorf("awsplatform: create bucket %s: %w", bucket, createErr)
		}
	}
	if versioned {
		_, err := s.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
			Bucket: aws.String(bucket),
			VersioningConfiguration: &s3types.VersioningConfiguration{
				Status: s3types.BucketVersioningStatusEnabled,
			},
		})
		if err != nil {
			return fmt.Errorf("awsplatform: enable versioning on %s: %w", bucket, err)
		}
	}
	return nil
}

// DefinitionKey returns the canonical workspace-scoped object key for a compiled bot definition
// (ADR 0001 & ADR 0003: workspaces/{workspace_id}/bots/{bot_id}/{version}.pb).
func DefinitionKey(workspaceID, botID, version string) (string, error) {
	if err := validateSegments(workspaceID, botID, version); err != nil {
		return "", err
	}
	return fmt.Sprintf("workspaces/%s/bots/%s/%s.pb", workspaceID, botID, version), nil
}

// StoredDefinition represents a compiled bot definition stored in S3.
type StoredDefinition struct {
	Bucket       string
	Key          string
	URI          string
	VersionID    string
	SHA256       string
	SignatureB64 string
	Bytes        []byte
}

// PutDefinition uploads a compiled Protobuf bot definition to S3 (migrating
// ContentDeployService.uploadPickleFile and bot_engine signature files), signing it
// with KMS when a signer and key ID are configured.
func (s *S3Service) PutDefinition(ctx context.Context, bucket, workspaceID, botID, version string, pbBytes []byte) (*StoredDefinition, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	key, err := DefinitionKey(workspaceID, botID, version)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pbBytes)
	shaHex := hex.EncodeToString(sum[:])

	metadata := map[string]string{
		"workspace-id": workspaceID,
		"bot-id":       botID,
		"version":      version,
		"sha256":       shaHex,
	}

	var sigB64 string
	if s.signer != nil && s.kmsKeyID != "" {
		sig, err := s.signer.SignArtifact(ctx, s.kmsKeyID, pbBytes)
		if err != nil {
			return nil, fmt.Errorf("awsplatform: sign bot definition: %w", err)
		}
		sigB64 = base64.StdEncoding.EncodeToString(sig)
		metadata["signature-b64"] = sigB64
		metadata["kms-key-id"] = s.kmsKeyID
	}

	out, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(pbBytes),
		ContentType: aws.String("application/x-protobuf"),
		Metadata:    metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: put definition s3://%s/%s: %w", bucket, key, err)
	}

	return &StoredDefinition{
		Bucket:       bucket,
		Key:          key,
		URI:          fmt.Sprintf("s3://%s/%s", bucket, key),
		VersionID:    aws.ToString(out.VersionId),
		SHA256:       shaHex,
		SignatureB64: sigB64,
		Bytes:        pbBytes,
	}, nil
}

// GetDefinition downloads and verifies a compiled bot definition from S3 (migrating
// bot_engine/src/utils.py load_pickle, nodes_validator_lambda, and pickle_rollback_lambda).
// When s3VersionID is non-empty, that specific S3 object version is retrieved.
func (s *S3Service) GetDefinition(ctx context.Context, bucket, workspaceID, botID, version, s3VersionID string) (*StoredDefinition, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	key, err := DefinitionKey(workspaceID, botID, version)
	if err != nil {
		return nil, err
	}
	in := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if s3VersionID != "" {
		in.VersionId = aws.String(s3VersionID)
	}
	out, err := s.client.GetObject(ctx, in)
	if err != nil {
		if isNotFoundError(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("awsplatform: get definition s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("awsplatform: read definition body: %w", err)
	}

	sum := sha256.Sum256(data)
	actualSHA := hex.EncodeToString(sum[:])
	if expectedSHA := out.Metadata["sha256"]; expectedSHA != "" && !strings.EqualFold(expectedSHA, actualSHA) {
		return nil, fmt.Errorf("%w: sha256=%s, want %s", ErrSignatureInvalid, actualSHA, expectedSHA)
	}

	sigB64 := out.Metadata["signature-b64"]
	if s.signer != nil && s.kmsKeyID != "" {
		if sigB64 == "" {
			return nil, fmt.Errorf("%w: missing KMS signature metadata", ErrSignatureInvalid)
		}
		sig, err := base64.StdEncoding.DecodeString(sigB64)
		if err != nil {
			return nil, fmt.Errorf("%w: decode signature: %v", ErrSignatureInvalid, err)
		}
		keyID := out.Metadata["kms-key-id"]
		if keyID == "" {
			keyID = s.kmsKeyID
		}
		if err := s.signer.VerifyArtifact(ctx, keyID, data, sig); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSignatureInvalid, err)
		}
	}

	return &StoredDefinition{
		Bucket:       bucket,
		Key:          key,
		URI:          fmt.Sprintf("s3://%s/%s", bucket, key),
		VersionID:    aws.ToString(out.VersionId),
		SHA256:       actualSHA,
		SignatureB64: sigB64,
		Bytes:        data,
	}, nil
}

// ObjectVersionEntry describes an S3 object version returned by ListDefinitionVersions
// (migrating F:\artemis\pickle_rollback_lambda\src\models\bucket_utils.py).
type ObjectVersionEntry struct {
	Key       string
	VersionID string
	IsLatest  bool
	Size      int64
}

// ListDefinitionVersions lists S3 object versions for a bot's definitions in a workspace
// (migrating BucketUtils.get_object_versions in pickle_rollback_lambda).
func (s *S3Service) ListDefinitionVersions(ctx context.Context, bucket, workspaceID, botID string, maxKeys int32) ([]ObjectVersionEntry, error) {
	if err := validateSegments(bucket, workspaceID, botID); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("workspaces/%s/bots/%s/", workspaceID, botID)
	in := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	if maxKeys > 0 {
		in.MaxKeys = aws.Int32(maxKeys)
	}
	out, err := s.client.ListObjectVersions(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("awsplatform: list definition versions: %w", err)
	}
	entries := make([]ObjectVersionEntry, 0, len(out.Versions))
	for _, v := range out.Versions {
		entries = append(entries, ObjectVersionEntry{
			Key:       aws.ToString(v.Key),
			VersionID: aws.ToString(v.VersionId),
			IsLatest:  aws.ToBool(v.IsLatest),
			Size:      aws.ToInt64(v.Size),
		})
	}
	return entries, nil
}

// NormalizeChannel normalizes channel names the same way as F:\artemis MultimediaService
// (e.g. "WebChat/Teams" -> "webchat-teams").
func NormalizeChannel(channel string) string {
	ch := strings.TrimSpace(channel)
	if strings.EqualFold(ch, "WebChat/Teams") {
		return "webchat-teams"
	}
	ch = strings.ReplaceAll(ch, "/", "-")
	return strings.ToLower(ch)
}

// MultimediaKey returns the workspace-scoped S3 key for a bot's multimedia asset
// (migrating F:\artemis MultimediaService and media_admin AwsBucketManagement under ADR 0003).
func MultimediaKey(workspaceID, botID, channel, mediaType, fileName string) (string, error) {
	normChannel := NormalizeChannel(channel)
	normType := strings.ToLower(strings.TrimSpace(mediaType))
	cleanFile := strings.TrimSpace(fileName)
	if err := validateSegments(workspaceID, botID, normChannel, normType, cleanFile); err != nil {
		return "", err
	}
	return fmt.Sprintf("workspaces/%s/bots/%s/media/%s/%s/%s",
		workspaceID, strings.ToLower(botID), normChannel, normType, cleanFile), nil
}

// MultimediaObject represents a multimedia file stored in S3.
type MultimediaObject struct {
	Key         string `json:"key"`
	FileName    string `json:"file_name"`
	Channel     string `json:"channel"`
	MediaType   string `json:"media_type"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	Body        []byte `json:"-"`
}

// UploadMultimedia stores a multimedia file in S3 under the workspace's prefix
// (migrating F:\artemis MultimediaService.sendMultimedia and AwsBucketManagement._save_file).
func (s *S3Service) UploadMultimedia(ctx context.Context, bucket, workspaceID, botID, channel, mediaType, fileName, contentType string, data []byte) (*MultimediaObject, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	key, err := MultimediaKey(workspaceID, botID, channel, mediaType, fileName)
	if err != nil {
		return nil, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		Metadata: map[string]string{
			"workspace-id": workspaceID,
			"bot-id":       botID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: upload multimedia s3://%s/%s: %w", bucket, key, err)
	}
	return &MultimediaObject{
		Key:         key,
		FileName:    fileName,
		Channel:     NormalizeChannel(channel),
		MediaType:   strings.ToLower(strings.TrimSpace(mediaType)),
		ContentType: contentType,
		Size:        int64(len(data)),
	}, nil
}

// GetMultimedia retrieves a multimedia object from S3 (migrating F:\artemis
// GetMultimediaService.getFile and media_admin AwsBucketManagement.retrieve_file_data).
func (s *S3Service) GetMultimedia(ctx context.Context, bucket, workspaceID, botID, channel, mediaType, fileName string) (*MultimediaObject, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	key, err := MultimediaKey(workspaceID, botID, channel, mediaType, fileName)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("awsplatform: get multimedia s3://%s/%s: %w", bucket, key, err)
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("awsplatform: read multimedia body: %w", err)
	}
	return &MultimediaObject{
		Key:         key,
		FileName:    fileName,
		Channel:     NormalizeChannel(channel),
		MediaType:   strings.ToLower(strings.TrimSpace(mediaType)),
		ContentType: aws.ToString(out.ContentType),
		Size:        int64(len(body)),
		Body:        body,
	}, nil
}

// DeleteMultimedia deletes a multimedia file from S3 (migrating F:\artemis
// MultimediaService.deleteMultimedia).
func (s *S3Service) DeleteMultimedia(ctx context.Context, bucket, workspaceID, botID, channel, mediaType, fileName string) error {
	if err := validateSegment(bucket); err != nil {
		return err
	}
	key, err := MultimediaKey(workspaceID, botID, channel, mediaType, fileName)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("awsplatform: delete multimedia s3://%s/%s: %w", bucket, key, err)
	}
	return nil
}

// RenameMultimedia copies an existing multimedia object to a new file name and deletes
// the old object (migrating F:\artemis MultimediaService.renameFileOnBucket).
func (s *S3Service) RenameMultimedia(ctx context.Context, bucket, workspaceID, botID, channel, mediaType, oldFileName, newFileName string) (*MultimediaObject, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	oldKey, err := MultimediaKey(workspaceID, botID, channel, mediaType, oldFileName)
	if err != nil {
		return nil, err
	}
	newKey, err := MultimediaKey(workspaceID, botID, channel, mediaType, newFileName)
	if err != nil {
		return nil, err
	}
	copySource := fmt.Sprintf("%s/%s", bucket, oldKey)
	_, err = s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(newKey),
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("awsplatform: copy multimedia %s to %s: %w", oldKey, newKey, err)
	}
	if err := s.DeleteMultimedia(ctx, bucket, workspaceID, botID, channel, mediaType, oldFileName); err != nil {
		return nil, err
	}
	return &MultimediaObject{
		Key:       newKey,
		FileName:  newFileName,
		Channel:   NormalizeChannel(channel),
		MediaType: strings.ToLower(strings.TrimSpace(mediaType)),
	}, nil
}

// ListMultimedia lists multimedia files for a bot in a workspace (migrating F:\artemis
// GetMultimediaService.listObjects).
func (s *S3Service) ListMultimedia(ctx context.Context, bucket, workspaceID, botID string) ([]MultimediaObject, error) {
	if err := validateSegments(bucket, workspaceID, botID); err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("workspaces/%s/bots/%s/media/", workspaceID, strings.ToLower(botID))
	out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: list multimedia: %w", err)
	}
	var items []MultimediaObject
	for _, obj := range out.Contents {
		k := aws.ToString(obj.Key)
		size := aws.ToInt64(obj.Size)
		if size == 0 && strings.HasSuffix(k, "/") {
			continue
		}
		rel := strings.TrimPrefix(k, prefix)
		parts := strings.SplitN(rel, "/", 3)
		var ch, mType, fName string
		if len(parts) == 3 {
			ch, mType, fName = parts[0], parts[1], parts[2]
		} else {
			fName = path.Base(k)
		}
		items = append(items, MultimediaObject{
			Key:       k,
			FileName:  fName,
			Channel:   ch,
			MediaType: mType,
			Size:      size,
		})
	}
	return items, nil
}

// ValidateMediaSize checks that the total size of multimedia objects is below maxBytes
// (migrating F:\artemis GetMultimediaService.validateMediaSize).
func ValidateMediaSize(objects []MultimediaObject, maxBytes int64) bool {
	if maxBytes <= 0 {
		maxBytes = MaxMultimediaTotalSizeBytes
	}
	var total int64
	for _, obj := range objects {
		total += obj.Size
	}
	return total < maxBytes
}

// CampaignFileKey returns the workspace-scoped S3 key for a campaign audience file
// (migrating F:\artemis CampaignsService.sendToBucket & ingester_lambda).
func CampaignFileKey(workspaceID, fileName string) (string, error) {
	if err := validateSegments(workspaceID, fileName); err != nil {
		return "", err
	}
	return fmt.Sprintf("workspaces/%s/campaigns/%s", workspaceID, fileName), nil
}

// UploadCampaignFile uploads a campaign file to S3 under the workspace's prefix.
func (s *S3Service) UploadCampaignFile(ctx context.Context, bucket, workspaceID, fileName string, data []byte) (string, error) {
	if err := validateSegment(bucket); err != nil {
		return "", err
	}
	key, err := CampaignFileKey(workspaceID, fileName)
	if err != nil {
		return "", err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
		Metadata: map[string]string{
			"workspace-id": workspaceID,
		},
	})
	if err != nil {
		return "", fmt.Errorf("awsplatform: upload campaign file: %w", err)
	}
	return fmt.Sprintf("s3://%s/%s", bucket, key), nil
}

// GetCampaignFile reads a campaign file from S3 (migrating ingester_lambda source_data_management.py).
func (s *S3Service) GetCampaignFile(ctx context.Context, bucket, workspaceID, fileName string) ([]byte, error) {
	if err := validateSegment(bucket); err != nil {
		return nil, err
	}
	key, err := CampaignFileKey(workspaceID, fileName)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("awsplatform: get campaign file: %w", err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DeleteCampaignFile removes a processed campaign file from S3 (migrating ingester_lambda).
func (s *S3Service) DeleteCampaignFile(ctx context.Context, bucket, workspaceID, fileName string) error {
	if err := validateSegment(bucket); err != nil {
		return err
	}
	key, err := CampaignFileKey(workspaceID, fileName)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

func validateSegments(segments ...string) error {
	for _, seg := range segments {
		if err := validateSegment(seg); err != nil {
			return err
		}
	}
	return nil
}

func validateSegment(seg string) error {
	s := strings.TrimSpace(seg)
	if s == "" || s == "." || s == ".." || strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return fmt.Errorf("%w: %q", ErrInvalidScope, seg)
	}
	return nil
}

func isNotFoundError(err error) bool {
	var noSuchKey *s3types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var noSuchBucket *s3types.NoSuchBucket
	if errors.As(err, &noSuchBucket) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket", "NoSuchVersion":
			return true
		}
	}
	return false
}

func isBucketAlreadyExists(err error) bool {
	var owned *s3types.BucketAlreadyOwnedByYou
	if errors.As(err, &owned) {
		return true
	}
	var exists *s3types.BucketAlreadyExists
	if errors.As(err, &exists) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return true
		}
	}
	return false
}
