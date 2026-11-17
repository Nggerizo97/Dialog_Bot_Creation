package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type campS3 struct {
	files map[string][]byte
}

func (c *campS3) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, nil
}
func (c *campS3) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}
func (c *campS3) PutBucketVersioning(context.Context, *s3.PutBucketVersioningInput, ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error) {
	return &s3.PutBucketVersioningOutput{}, nil
}
func (c *campS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if c.files == nil {
		c.files = map[string][]byte{}
	}
	b, _ := io.ReadAll(in.Body)
	c.files[aws.ToString(in.Key)] = b
	return &s3.PutObjectOutput{}, nil
}
func (c *campS3) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	b, ok := c.files[aws.ToString(in.Key)]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(b))}, nil
}
func (c *campS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(c.files, aws.ToString(in.Key))
	return &s3.DeleteObjectOutput{}, nil
}
func (c *campS3) CopyObject(context.Context, *s3.CopyObjectInput, ...func(*s3.Options)) (*s3.CopyObjectOutput, error) {
	return &s3.CopyObjectOutput{}, nil
}
func (c *campS3) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{}, nil
}
func (c *campS3) ListObjectVersions(context.Context, *s3.ListObjectVersionsInput, ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{}, nil
}

type campSQS struct {
	msgs []sqstypes.Message
}

func (q *campSQS) CreateQueue(context.Context, *sqs.CreateQueueInput, ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	return &sqs.CreateQueueOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/campaigns")}, nil
}
func (q *campSQS) GetQueueUrl(context.Context, *sqs.GetQueueUrlInput, ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	return &sqs.GetQueueUrlOutput{QueueUrl: aws.String("https://127.0.0.1:9297/000000000000/campaigns")}, nil
}
func (q *campSQS) SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return &sqs.SendMessageOutput{}, nil
}
func (q *campSQS) SendMessageBatch(_ context.Context, in *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	var ok []sqstypes.SendMessageBatchResultEntry
	for i, e := range in.Entries {
		id := fmt.Sprintf("m-%d-%d", len(q.msgs), i)
		q.msgs = append(q.msgs, sqstypes.Message{
			MessageId:         aws.String(id),
			ReceiptHandle:     aws.String("rh-" + id),
			Body:              e.MessageBody,
			MessageAttributes: e.MessageAttributes,
		})
		ok = append(ok, sqstypes.SendMessageBatchResultEntry{Id: e.Id, MessageId: aws.String(id)})
	}
	return &sqs.SendMessageBatchOutput{Successful: ok}, nil
}
func (q *campSQS) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	n := min(int(in.MaxNumberOfMessages), len(q.msgs))
	return &sqs.ReceiveMessageOutput{Messages: append([]sqstypes.Message(nil), q.msgs[:n]...)}, nil
}
func (q *campSQS) DeleteMessageBatch(_ context.Context, in *sqs.DeleteMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	drop := map[string]bool{}
	var ok []sqstypes.DeleteMessageBatchResultEntry
	for _, e := range in.Entries {
		drop[aws.ToString(e.ReceiptHandle)] = true
		ok = append(ok, sqstypes.DeleteMessageBatchResultEntry{Id: e.Id})
	}
	var kept []sqstypes.Message
	for _, m := range q.msgs {
		if !drop[aws.ToString(m.ReceiptHandle)] {
			kept = append(kept, m)
		}
	}
	q.msgs = kept
	return &sqs.DeleteMessageBatchOutput{Successful: ok}, nil
}

func TestCampaignUploadIngestAndConsume(t *testing.T) {
	ctx := context.Background()
	s3Backend := &campS3{}
	sqsBackend := &campSQS{}
	disp := NewCampaignDispatcher(
		"bot-dialog-generator-campaigns",
		"https://127.0.0.1:9297/000000000000/campaigns",
		awsplatform.NewS3Service(s3Backend, nil, ""),
		awsplatform.NewSQSService(sqsBackend),
	)

	var csvBuilder bytes.Buffer
	csvBuilder.WriteString("phone,name\n")
	for i := range 12 {
		csvBuilder.WriteString("+573000000" + strconv.Itoa(i) + ",User" + strconv.Itoa(i) + "\n")
	}

	res, err := disp.UploadAndIngestCSV(ctx, "ws-customer-service", "camp-1", "october.csv", csvBuilder.Bytes())
	if err != nil || res.Successful != 12 || res.Failed != 0 {
		t.Fatalf("UploadAndIngestCSV = %+v, err = %v", res, err)
	}
	// Processed campaign CSV must be deleted from S3 (matching ingester_lambda behavior)
	if len(s3Backend.files) != 0 {
		t.Fatalf("expected S3 campaign file to be deleted after ingestion, remaining: %v", s3Backend.files)
	}

	var consumed int
	n, err := disp.ConsumeBatch(ctx, 10, 0, func(m awsplatform.QueueMessage) error {
		if m.WorkspaceID != "ws-customer-service" {
			return errors.New("wrong workspace_id")
		}
		consumed++
		return nil
	})
	if err != nil || n != 10 || consumed != 10 {
		t.Fatalf("ConsumeBatch = %d, consumed = %d, err = %v", n, consumed, err)
	}
	if len(sqsBackend.msgs) != 2 {
		t.Fatalf("expected 2 messages remaining in SQS, got %d", len(sqsBackend.msgs))
	}
}
