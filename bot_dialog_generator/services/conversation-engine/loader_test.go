package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"io"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef"
	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type memS3 struct {
	objs map[string]*s3.PutObjectInput
	gets int
}

func (m *memS3) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (m *memS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}
func (m *memS3) PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}
func (m *memS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.objs == nil {
		m.objs = map[string]*s3.PutObjectInput{}
	}
	b, _ := io.ReadAll(in.Body)
	cp := *in
	cp.Body = bytes.NewReader(b)
	m.objs[aws.ToString(in.Bucket)+"/"+aws.ToString(in.Key)] = &cp
	return &s3.PutObjectOutput{VersionId: aws.String("v1")}, nil
}
func (m *memS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.gets++
	put, ok := m.objs[aws.ToString(in.Bucket)+"/"+aws.ToString(in.Key)]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	b, _ := io.ReadAll(put.Body)
	put.Body = bytes.NewReader(b)
	return &s3.GetObjectOutput{
		Body:     io.NopCloser(bytes.NewReader(b)),
		Metadata: put.Metadata,
	}, nil
}
func (m *memS3) DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return &s3.DeleteObjectOutput{}, nil
}
func (m *memS3) CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	return &s3.CopyObjectOutput{}, nil
}
func (m *memS3) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{}, nil
}
func (m *memS3) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}

type hmacSigner struct{}

func (hmacSigner) SignArtifact(_ context.Context, keyID string, payload []byte) ([]byte, error) {
	h := hmac.New(sha256.New, []byte(keyID))
	h.Write(payload)
	return h.Sum(nil), nil
}
func (s hmacSigner) VerifyArtifact(ctx context.Context, keyID string, payload, signature []byte) error {
	want, _ := s.SignArtifact(ctx, keyID, payload)
	if !hmac.Equal(want, signature) {
		return awsplatform.ErrSignatureInvalid
	}
	return nil
}

type memSQS struct {
	messages []string
}

func (q *memSQS) CreateQueue(context.Context, *sqs.CreateQueueInput, ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	return &sqs.CreateQueueOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/events")}, nil
}
func (q *memSQS) GetQueueUrl(context.Context, *sqs.GetQueueUrlInput, ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/events")}, nil
}
func (q *memSQS) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	q.messages = append(q.messages, aws.ToString(in.MessageBody))
	return &sqs.SendMessageOutput{MessageId: aws.String("m1")}, nil
}
func (q *memSQS) SendMessageBatch(context.Context, *sqs.SendMessageBatchInput, ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	return &sqs.SendMessageBatchOutput{}, nil
}
func (q *memSQS) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}
func (q *memSQS) DeleteMessageBatch(context.Context, *sqs.DeleteMessageBatchInput, ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	return &sqs.DeleteMessageBatchOutput{}, nil
}

func TestDefinitionLoaderVerifiesAndRunsStep(t *testing.T) {
	ctx := context.Background()
	compiled, err := botdef.Compile(&botdef.DraftVersion{
		Tenant:      "demo",
		WorkspaceID: "ws-hr",
		AppID:       "hr-helpdesk",
		Version:     "v1",
		EntryNodeID: "welcome",
		Nodes: []botdef.DraftNode{
			{ID: "welcome", Type: "Trigger", Title: "Welcome"},
			{ID: "greet", Type: "Response", Title: "Greet", Properties: map[string]string{"message": "Hello from S3!"}},
		},
		Edges: []botdef.DraftEdge{{From: "welcome", To: "greet"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	s3Backend := &memS3{}
	s3Svc := awsplatform.NewS3Service(s3Backend, hmacSigner{}, "alias/bdg-key")
	const bucket = "bot-dialog-generator-definitions"
	if _, err := s3Svc.PutDefinition(ctx, bucket, "ws-hr", "hr-helpdesk", "v1", compiled.Bytes); err != nil {
		t.Fatal(err)
	}

	sqsBackend := &memSQS{}
	sqsSvc := awsplatform.NewSQSService(sqsBackend)
	loader := NewDefinitionLoader(bucket, s3Svc, sqsSvc, "https://127.0.0.1:9297/000000000000/events")

	def, err := loader.Load(ctx, "ws-hr", "hr-helpdesk", "v1")
	if err != nil {
		t.Fatal(err)
	}
	// Second load hits cache without an extra S3 GetObject call
	if _, err := loader.Load(ctx, "ws-hr", "hr-helpdesk", "v1"); err != nil {
		t.Fatal(err)
	}
	if s3Backend.gets != 1 {
		t.Fatalf("expected 1 S3 GetObject call (cached), got %d", s3Backend.gets)
	}

	res, err := Step(def, &Session{SessionID: "s1", UserID: "u1", Tenant: "demo", WorkspaceID: "ws-hr", Channel: "webchat"}, &botdialoggeneratorv1.InboundMessage{
		MessageId:   "m1",
		Tenant:      "demo",
		WorkspaceId: "ws-hr",
		Channel:     "webchat",
		UserId:      "u1",
		Body:        &botdialoggeneratorv1.InboundMessage_Text{Text: &botdialoggeneratorv1.Text{Value: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Batch.Messages) != 1 || res.Batch.Messages[0].GetText().GetValue() != "Hello from S3!" {
		t.Fatalf("unexpected step output: %+v", res.Batch)
	}
	if _, err := loader.EmitTurnEvent(ctx, "ws-hr", map[string]any{"session_id": "s1", "turns": res.Session.Turns}); err != nil {
		t.Fatal(err)
	}
	if len(sqsBackend.messages) != 1 {
		t.Fatalf("expected 1 SQS turn event, got %d", len(sqsBackend.messages))
	}
}
