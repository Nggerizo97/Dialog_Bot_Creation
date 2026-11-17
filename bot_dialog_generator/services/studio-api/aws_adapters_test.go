package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/platform/auth"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type testS3Backend struct {
	objects map[string]*s3.PutObjectInput
}

func (b *testS3Backend) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (b *testS3Backend) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}
func (b *testS3Backend) PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}
func (b *testS3Backend) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if b.objects == nil {
		b.objects = map[string]*s3.PutObjectInput{}
	}
	data, _ := io.ReadAll(in.Body)
	cp := *in
	cp.Body = bytes.NewReader(data)
	b.objects[aws.ToString(in.Bucket)+"/"+aws.ToString(in.Key)] = &cp
	return &s3.PutObjectOutput{VersionId: aws.String("v1")}, nil
}
func (b *testS3Backend) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	put, ok := b.objects[aws.ToString(in.Bucket)+"/"+aws.ToString(in.Key)]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	data, _ := io.ReadAll(put.Body)
	put.Body = bytes.NewReader(data)
	return &s3.GetObjectOutput{
		Body:     io.NopCloser(bytes.NewReader(data)),
		Metadata: put.Metadata,
	}, nil
}
func (b *testS3Backend) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return &s3.DeleteObjectOutput{}, nil
}
func (b *testS3Backend) CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	return &s3.CopyObjectOutput{}, nil
}
func (b *testS3Backend) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{}, nil
}
func (b *testS3Backend) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}

type testKMSBackend struct{}

func (testKMSBackend) CreateKey(context.Context, *kms.CreateKeyInput, ...func(*kms.Options)) (*kms.CreateKeyOutput, error) {
	return &kms.CreateKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String("kms-key-1")}}, nil
}
func (testKMSBackend) CreateAlias(context.Context, *kms.CreateAliasInput, ...func(*kms.Options)) (*kms.CreateAliasOutput, error) {
	return &kms.CreateAliasOutput{}, nil
}
func (testKMSBackend) DescribeKey(context.Context, *kms.DescribeKeyInput, ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	return &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{KeyId: aws.String("kms-key-1")}}, nil
}
func (testKMSBackend) Sign(_ context.Context, in *kms.SignInput, _ ...func(*kms.Options)) (*kms.SignOutput, error) {
	mac := hmac.New(sha256.New, []byte("studio-kms-secret"))
	mac.Write(in.Message)
	return &kms.SignOutput{Signature: mac.Sum(nil)}, nil
}
func (k testKMSBackend) Verify(ctx context.Context, in *kms.VerifyInput, _ ...func(*kms.Options)) (*kms.VerifyOutput, error) {
	sig, _ := k.Sign(ctx, &kms.SignInput{Message: in.Message})
	return &kms.VerifyOutput{SignatureValid: hmac.Equal(sig.Signature, in.Signature)}, nil
}
func (testKMSBackend) Encrypt(context.Context, *kms.EncryptInput, ...func(*kms.Options)) (*kms.EncryptOutput, error) {
	return &kms.EncryptOutput{}, nil
}
func (testKMSBackend) Decrypt(context.Context, *kms.DecryptInput, ...func(*kms.Options)) (*kms.DecryptOutput, error) {
	return &kms.DecryptOutput{}, nil
}

type testSQSBackend struct {
	sent []string
}

func (q *testSQSBackend) CreateQueue(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	return &sqs.CreateQueueOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/" + aws.ToString(in.QueueName))}, nil
}
func (q *testSQSBackend) GetQueueUrl(_ context.Context, in *sqs.GetQueueUrlInput, _ ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/" + aws.ToString(in.QueueName))}, nil
}
func (q *testSQSBackend) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	q.sent = append(q.sent, aws.ToString(in.MessageBody))
	return &sqs.SendMessageOutput{MessageId: aws.String(fmt.Sprintf("m-%d", len(q.sent)))}, nil
}
func (q *testSQSBackend) SendMessageBatch(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	return &sqs.SendMessageBatchOutput{}, nil
}
func (q *testSQSBackend) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}
func (q *testSQSBackend) DeleteMessageBatch(context.Context, *sqs.DeleteMessageBatchInput, ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	return &sqs.DeleteMessageBatchOutput{}, nil
}

func TestPublishUploadsKMSSignedDefinitionToS3AndRelaysSQS(t *testing.T) {
	iss, err := auth.NewDevIssuer("studio-api")
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore()
	s3Backend := &testS3Backend{}
	kmsSvc := awsplatform.NewKMSService(testKMSBackend{})
	s3Svc := awsplatform.NewS3Service(s3Backend, kmsSvc, "alias/bot-dialog-generator-signing")
	sqsBackend := &testSQSBackend{}
	sqsSvc := awsplatform.NewSQSService(sqsBackend)

	pub := NewAWSArtifactPublisher("bot-dialog-generator-definitions", s3Svc, sqsSvc, "https://127.0.0.1:9297/000000000000/bdg-events")
	handler := NewStudioHandler(store, Config{
		Verifier:           iss,
		DevIssuer:          iss,
		PlatformAdminGroup: adminGroup,
		Artifacts:          pub,
	})

	tok, err := iss.Mint("alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/workspaces/ws-customer-service/bots/retail-assistant/versions/v18/publish", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish status = %d: %s", rec.Code, rec.Body.String())
	}
	body := decode[map[string]any](t, rec)
	if body["signature_b64"] == "" || body["signature_b64"] == nil {
		t.Fatalf("expected signature_b64 in publish response, got %+v", body)
	}

	// Verify the stored S3 object can be retrieved and passes KMS signature check
	stored, err := s3Svc.GetDefinition(context.Background(), "bot-dialog-generator-definitions", "ws-customer-service", "retail-assistant", "v18", "")
	if err != nil {
		t.Fatalf("GetDefinition failed: %v", err)
	}
	if stored.SHA256 != body["sha256"] {
		t.Fatalf("sha256 = %s, want %v", stored.SHA256, body["sha256"])
	}
	if len(sqsBackend.sent) != 1 || !strings.Contains(sqsBackend.sent[0], `"bot.published"`) {
		t.Fatalf("expected bot.published SQS event, got %+v", sqsBackend.sent)
	}
}

func TestResolveDatabaseURLFromSecretsManager(t *testing.T) {
	url, err := resolveDatabaseURLFromSecret(context.Background(), func(k string) string {
		if k == "DB_SECRET_ID" {
			return "bdg/local/db"
		}
		return ""
	}, func(_ context.Context, id string) (awsplatform.DBSecret, error) {
		if id != "bdg/local/db" {
			t.Fatalf("unexpected secret ID %q", id)
		}
		return awsplatform.DBSecret{
			Username: "studio_api",
			Password: "studio_api_local",
			Host:     "localhost",
			Port:     5433,
			DBName:   "bot_dialog_generator",
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if url != "postgres://studio_api:studio_api_local@localhost:5433/bot_dialog_generator" {
		t.Fatalf("url = %q", url)
	}
}
