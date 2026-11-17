package awsplatform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
	// MaxSQSBatchSize is the maximum batch size supported by AWS SQS (10 messages),
	// matching F:\artemis\campaigns\src\libs\queue_service.py and scheduler_lambda.
	MaxSQSBatchSize = 10
)

// SQSAPI is the subset of the AWS SQS client used by SQSService.
type SQSAPI interface {
	CreateQueue(ctx context.Context, params *sqs.CreateQueueInput, optFns ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	SendMessageBatch(ctx context.Context, params *sqs.SendMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, params *sqs.DeleteMessageBatchInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
}

// SQSService migrates the SQS queue clients from F:\artemis:
//   - bot_engine/src/libs/sqs.py (single event enqueue)
//   - campaigns/src/libs/queue_service.py (long-polling receive & batch delete)
//   - scheduler_lambda/src/classes/queue_class.py (chunked batch send with counts)
//   - pipeline_trigger_lambda/src/models/email_sender.py (notification enqueue)
type SQSService struct {
	client SQSAPI
}

// NewSQSService wraps an AWS SQS client.
func NewSQSService(client SQSAPI) *SQSService {
	return &SQSService{client: client}
}

// EnsureQueue resolves the queue URL by name, creating the queue if it does not exist.
func (q *SQSService) EnsureQueue(ctx context.Context, queueName string) (string, error) {
	if err := validateSegment(queueName); err != nil {
		return "", err
	}
	if out, err := q.client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(queueName)}); err == nil && out.QueueUrl != nil {
		return aws.ToString(out.QueueUrl), nil
	}
	created, err := q.client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(queueName)})
	if err != nil {
		return "", fmt.Errorf("awsplatform: create queue %s: %w", queueName, err)
	}
	return aws.ToString(created.QueueUrl), nil
}

// SendJSON marshals item as JSON and sends it to the queue with a workspace_id attribute
// (migrating F:\artemis\bot_engine\src\libs\sqs.py SQS.put).
func (q *SQSService) SendJSON(ctx context.Context, queueURL, workspaceID string, item any) (string, error) {
	if strings.TrimSpace(queueURL) == "" {
		return "", errors.New("awsplatform: queueURL is required")
	}
	if err := validateSegment(workspaceID); err != nil {
		return "", err
	}
	body, err := json.Marshal(item)
	if err != nil {
		return "", fmt.Errorf("awsplatform: marshal SQS message: %w", err)
	}
	out, err := q.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
		MessageAttributes: map[string]sqstypes.MessageAttributeValue{
			"workspace_id": {
				DataType:    aws.String("String"),
				StringValue: aws.String(workspaceID),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("awsplatform: send SQS message: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}

// BatchResult summarizes the outcome of a chunked batch send (matching
// F:\artemis\scheduler_lambda\src\classes\queue_class.py QueueManagement.send_messages).
type BatchResult struct {
	Successful int `json:"successful"`
	Failed     int `json:"failed"`
}

// SendBatch sends arbitrary slices of payloads in chunks of up to 10 (MaxSQSBatchSize)
// and aggregates Successful and Failed counts, migrating QueueManagement.send_messages
// from F:\artemis\scheduler_lambda\src\classes\queue_class.py.
func (q *SQSService) SendBatch(ctx context.Context, queueURL, workspaceID string, payloads []any) (BatchResult, error) {
	if strings.TrimSpace(queueURL) == "" {
		return BatchResult{}, errors.New("awsplatform: queueURL is required")
	}
	if err := validateSegment(workspaceID); err != nil {
		return BatchResult{}, err
	}
	var res BatchResult
	for start := 0; start < len(payloads); start += MaxSQSBatchSize {
		end := min(start+MaxSQSBatchSize, len(payloads))
		chunk := payloads[start:end]
		entries := make([]sqstypes.SendMessageBatchRequestEntry, 0, len(chunk))
		for i, item := range chunk {
			body, err := json.Marshal(item)
			if err != nil {
				return res, fmt.Errorf("awsplatform: marshal batch entry %d: %w", start+i, err)
			}
			entries = append(entries, sqstypes.SendMessageBatchRequestEntry{
				Id:          aws.String(strconv.Itoa(i)),
				MessageBody: aws.String(string(body)),
				MessageAttributes: map[string]sqstypes.MessageAttributeValue{
					"workspace_id": {
						DataType:    aws.String("String"),
						StringValue: aws.String(workspaceID),
					},
				},
			})
		}
		out, err := q.client.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  entries,
		})
		if err != nil {
			return res, fmt.Errorf("awsplatform: send SQS batch: %w", err)
		}
		res.Successful += len(out.Successful)
		res.Failed += len(out.Failed)
	}
	return res, nil
}

// QueueMessage represents a message received from SQS.
type QueueMessage struct {
	MessageID     string
	ReceiptHandle string
	WorkspaceID   string
	Body          string
}

// ReceiveMessages receives up to maxMessages (1..10) from SQS using long polling
// (migrating F:\artemis\campaigns\src\libs\queue_service.py QueueService.get_messages).
func (q *SQSService) ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitTimeSeconds int32) ([]QueueMessage, error) {
	if strings.TrimSpace(queueURL) == "" {
		return nil, errors.New("awsplatform: queueURL is required")
	}
	if maxMessages <= 0 || maxMessages > MaxSQSBatchSize {
		maxMessages = MaxSQSBatchSize
	}
	if waitTimeSeconds < 0 {
		waitTimeSeconds = 0
	}
	out, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(queueURL),
		MaxNumberOfMessages:   maxMessages,
		WaitTimeSeconds:       waitTimeSeconds,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		return nil, fmt.Errorf("awsplatform: receive SQS messages: %w", err)
	}
	msgs := make([]QueueMessage, 0, len(out.Messages))
	for _, m := range out.Messages {
		var wsID string
		if attr, ok := m.MessageAttributes["workspace_id"]; ok {
			wsID = aws.ToString(attr.StringValue)
		}
		msgs = append(msgs, QueueMessage{
			MessageID:     aws.ToString(m.MessageId),
			ReceiptHandle: aws.ToString(m.ReceiptHandle),
			WorkspaceID:   wsID,
			Body:          aws.ToString(m.Body),
		})
	}
	return msgs, nil
}

// DeleteMessages deletes processed messages in a single batch call (migrating
// F:\artemis\campaigns\src\libs\queue_service.py QueueService.delete_messages).
func (q *SQSService) DeleteMessages(ctx context.Context, queueURL string, messages []QueueMessage) (BatchResult, error) {
	if strings.TrimSpace(queueURL) == "" {
		return BatchResult{}, errors.New("awsplatform: queueURL is required")
	}
	var res BatchResult
	for start := 0; start < len(messages); start += MaxSQSBatchSize {
		end := min(start+MaxSQSBatchSize, len(messages))
		chunk := messages[start:end]
		entries := make([]sqstypes.DeleteMessageBatchRequestEntry, 0, len(chunk))
		for i, m := range chunk {
			entries = append(entries, sqstypes.DeleteMessageBatchRequestEntry{
				Id:            aws.String(strconv.Itoa(i)),
				ReceiptHandle: aws.String(m.ReceiptHandle),
			})
		}
		out, err := q.client.DeleteMessageBatch(ctx, &sqs.DeleteMessageBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  entries,
		})
		if err != nil {
			return res, fmt.Errorf("awsplatform: delete SQS messages: %w", err)
		}
		res.Successful += len(out.Successful)
		res.Failed += len(out.Failed)
	}
	return res, nil
}
