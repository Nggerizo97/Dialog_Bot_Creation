package awsplatform

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"
)

// KMSAPI is the subset of the AWS KMS client used by KMSService.
type KMSAPI interface {
	CreateKey(ctx context.Context, params *kms.CreateKeyInput, optFns ...func(*kms.Options)) (*kms.CreateKeyOutput, error)
	CreateAlias(ctx context.Context, params *kms.CreateAliasInput, optFns ...func(*kms.Options)) (*kms.CreateAliasOutput, error)
	DescribeKey(ctx context.Context, params *kms.DescribeKeyInput, optFns ...func(*kms.Options)) (*kms.DescribeKeyOutput, error)
	Sign(ctx context.Context, params *kms.SignInput, optFns ...func(*kms.Options)) (*kms.SignOutput, error)
	Verify(ctx context.Context, params *kms.VerifyInput, optFns ...func(*kms.Options)) (*kms.VerifyOutput, error)
	Encrypt(ctx context.Context, params *kms.EncryptInput, optFns ...func(*kms.Options)) (*kms.EncryptOutput, error)
	Decrypt(ctx context.Context, params *kms.DecryptInput, optFns ...func(*kms.Options)) (*kms.DecryptOutput, error)
}

// KMSService migrates KMS encryption/decryption from F:\artemis\bot_engine\src\utils.py
// and implements KMS artifact signing and per-workspace keys (ADR 0001 & ADR 0003).
type KMSService struct {
	client KMSAPI
}

// NewKMSService creates a KMSService backed by an AWS KMS client.
func NewKMSService(client KMSAPI) *KMSService {
	return &KMSService{client: client}
}

// EnsureSigningKey returns the key ID for an asymmetric signing key alias (e.g.
// "alias/bot-dialog-generator-signing"), creating the key and alias if they do not exist.
func (k *KMSService) EnsureSigningKey(ctx context.Context, aliasName string) (string, error) {
	aliasName = normalizeAlias(aliasName)
	if out, err := k.client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(aliasName)}); err == nil && out.KeyMetadata != nil {
		return aws.ToString(out.KeyMetadata.KeyId), nil
	}
	created, err := k.client.CreateKey(ctx, &kms.CreateKeyInput{
		Description: aws.String("Bot_Dialog_Generator bot definition signing key (ADR 0001)"),
		KeyUsage:    kmstypes.KeyUsageTypeSignVerify,
		KeySpec:     kmstypes.KeySpecEccNistP256,
	})
	if err != nil {
		return "", fmt.Errorf("awsplatform: create KMS signing key: %w", err)
	}
	keyID := aws.ToString(created.KeyMetadata.KeyId)
	_, err = k.client.CreateAlias(ctx, &kms.CreateAliasInput{
		AliasName:   aws.String(aliasName),
		TargetKeyId: aws.String(keyID),
	})
	if err != nil && !isAlreadyExistsError(err) {
		return "", fmt.Errorf("awsplatform: create KMS alias %s: %w", aliasName, err)
	}
	return keyID, nil
}

// EnsureWorkspaceKey returns the symmetric encryption key ID for a workspace
// ("alias/bdg-workspace-<workspaceID>", ADR 0003), creating it if needed.
func (k *KMSService) EnsureWorkspaceKey(ctx context.Context, workspaceID string) (string, error) {
	if err := validateSegment(workspaceID); err != nil {
		return "", err
	}
	aliasName := "alias/bdg-workspace-" + workspaceID
	if out, err := k.client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(aliasName)}); err == nil && out.KeyMetadata != nil {
		return aws.ToString(out.KeyMetadata.KeyId), nil
	}
	created, err := k.client.CreateKey(ctx, &kms.CreateKeyInput{
		Description: aws.String("Bot_Dialog_Generator workspace key for " + workspaceID),
		KeyUsage:    kmstypes.KeyUsageTypeEncryptDecrypt,
		KeySpec:     kmstypes.KeySpecSymmetricDefault,
	})
	if err != nil {
		return "", fmt.Errorf("awsplatform: create workspace KMS key: %w", err)
	}
	keyID := aws.ToString(created.KeyMetadata.KeyId)
	_, err = k.client.CreateAlias(ctx, &kms.CreateAliasInput{
		AliasName:   aws.String(aliasName),
		TargetKeyId: aws.String(keyID),
	})
	if err != nil && !isAlreadyExistsError(err) {
		return "", fmt.Errorf("awsplatform: create workspace KMS alias %s: %w", aliasName, err)
	}
	return keyID, nil
}

// SignArtifact signs the SHA-256 digest of a compiled bot definition using KMS Sign.
func (k *KMSService) SignArtifact(ctx context.Context, keyID string, payload []byte) ([]byte, error) {
	if strings.TrimSpace(keyID) == "" {
		return nil, errors.New("awsplatform: KMS key ID is required")
	}
	digest := sha256.Sum256(payload)
	out, err := k.client.Sign(ctx, &kms.SignInput{
		KeyId:            aws.String(keyID),
		Message:          digest[:],
		MessageType:      kmstypes.MessageTypeDigest,
		SigningAlgorithm: kmstypes.SigningAlgorithmSpecEcdsaSha256,
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: KMS sign: %w", err)
	}
	return out.Signature, nil
}

// VerifyArtifact verifies the signature of a compiled bot definition using KMS Verify.
func (k *KMSService) VerifyArtifact(ctx context.Context, keyID string, payload, signature []byte) error {
	if strings.TrimSpace(keyID) == "" {
		return errors.New("awsplatform: KMS key ID is required")
	}
	digest := sha256.Sum256(payload)
	out, err := k.client.Verify(ctx, &kms.VerifyInput{
		KeyId:            aws.String(keyID),
		Message:          digest[:],
		MessageType:      kmstypes.MessageTypeDigest,
		Signature:        signature,
		SigningAlgorithm: kmstypes.SigningAlgorithmSpecEcdsaSha256,
	})
	if err != nil {
		return fmt.Errorf("awsplatform: KMS verify: %w", err)
	}
	if !out.SignatureValid {
		return ErrSignatureInvalid
	}
	return nil
}

// Encrypt encrypts plaintext bytes with the specified KMS key.
func (k *KMSService) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	if strings.TrimSpace(keyID) == "" {
		return nil, errors.New("awsplatform: KMS key ID is required")
	}
	out, err := k.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId:     aws.String(keyID),
		Plaintext: plaintext,
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: KMS encrypt: %w", err)
	}
	return out.CiphertextBlob, nil
}

// Decrypt decrypts a KMS ciphertext blob (migrating F:\artemis\bot_engine\src\utils.py decrypt).
func (k *KMSService) Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("awsplatform: empty ciphertext")
	}
	out, err := k.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: KMS decrypt: %w", err)
	}
	return out.Plaintext, nil
}

func normalizeAlias(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return "alias/bot-dialog-generator-signing"
	}
	if !strings.HasPrefix(n, "alias/") {
		return "alias/" + n
	}
	return n
}

func isAlreadyExistsError(err error) bool {
	var alreadyExists *kmstypes.AlreadyExistsException
	if errors.As(err, &alreadyExists) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "AlreadyExistsException" {
		return true
	}
	return false
}
