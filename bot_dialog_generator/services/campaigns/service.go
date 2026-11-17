package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
)

// CampaignRecord represents one recipient row parsed from a campaign file in S3.
type CampaignRecord struct {
	WorkspaceID string            `json:"workspace_id"`
	CampaignID  string            `json:"campaign_id"`
	Recipient   string            `json:"recipient"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

// CampaignDispatcher migrates the campaign S3 ingestion and SQS queue pipeline from F:\artemis:
//   - F:\artemis\back\src\modules\prototype\infra\services\campaigns.service.ts (S3 upload)
//   - F:\artemis\ingester_lambda\src\classes\source_data_management.py (S3 read, CSV parse, S3 delete)
//   - F:\artemis\scheduler_lambda\src\classes\queue_class.py (SQS chunked batch enqueue)
//   - F:\artemis\campaigns\src\libs\queue_service.py (SQS long-poll receive & batch delete)
type CampaignDispatcher struct {
	bucket   string
	queueURL string
	s3       *awsplatform.S3Service
	sqs      *awsplatform.SQSService
}

// NewCampaignDispatcher creates a CampaignDispatcher backed by S3 and SQS.
func NewCampaignDispatcher(bucket, queueURL string, s3Svc *awsplatform.S3Service, sqsSvc *awsplatform.SQSService) *CampaignDispatcher {
	return &CampaignDispatcher{
		bucket:   bucket,
		queueURL: queueURL,
		s3:       s3Svc,
		sqs:      sqsSvc,
	}
}

// UploadAndIngestCSV uploads a campaign CSV file to S3 under the workspace prefix,
// reads and parses its rows, enqueues them in chunks of 10 onto SQS, and deletes the
// staged S3 file upon completion (migrating campaigns.service.ts + ingester_lambda + scheduler_lambda).
func (d *CampaignDispatcher) UploadAndIngestCSV(ctx context.Context, workspaceID, campaignID, fileName string, csvData []byte) (awsplatform.BatchResult, error) {
	if _, err := d.s3.UploadCampaignFile(ctx, d.bucket, workspaceID, fileName, csvData); err != nil {
		return awsplatform.BatchResult{}, err
	}
	raw, err := d.s3.GetCampaignFile(ctx, d.bucket, workspaceID, fileName)
	if err != nil {
		return awsplatform.BatchResult{}, err
	}
	records, err := parseCampaignCSV(workspaceID, campaignID, raw)
	if err != nil {
		return awsplatform.BatchResult{}, err
	}
	payloads := make([]any, 0, len(records))
	for _, rec := range records {
		payloads = append(payloads, rec)
	}
	res, err := d.sqs.SendBatch(ctx, d.queueURL, workspaceID, payloads)
	if err != nil {
		return res, err
	}
	if err := d.s3.DeleteCampaignFile(ctx, d.bucket, workspaceID, fileName); err != nil {
		return res, fmt.Errorf("delete processed campaign file: %w", err)
	}
	return res, nil
}

// ConsumeBatch receives up to maxMessages from the campaign SQS queue and deletes them
// once processed (migrating F:\artemis\campaigns\src\libs\queue_service.py).
func (d *CampaignDispatcher) ConsumeBatch(ctx context.Context, maxMessages, waitTimeSeconds int32, handler func(awsplatform.QueueMessage) error) (int, error) {
	msgs, err := d.sqs.ReceiveMessages(ctx, d.queueURL, maxMessages, waitTimeSeconds)
	if err != nil {
		return 0, err
	}
	var ack []awsplatform.QueueMessage
	for _, m := range msgs {
		if err := handler(m); err != nil {
			return len(ack), err
		}
		ack = append(ack, m)
	}
	if len(ack) > 0 {
		if _, err := d.sqs.DeleteMessages(ctx, d.queueURL, ack); err != nil {
			return len(ack), err
		}
	}
	return len(ack), nil
}

func parseCampaignCSV(workspaceID, campaignID string, data []byte) ([]CampaignRecord, error) {
	r := csv.NewReader(bytes.NewReader(data))
	headers, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read campaign CSV header: %w", err)
	}
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}
	var out []CampaignRecord
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read campaign CSV row: %w", err)
		}
		if len(row) == 0 {
			continue
		}
		attrs := make(map[string]string, len(headers))
		for i, col := range headers {
			if i < len(row) {
				attrs[col] = strings.TrimSpace(row[i])
			}
		}
		recipient := attrs["phone"]
		if recipient == "" {
			recipient = attrs["recipient"]
		}
		if recipient == "" && len(row) > 0 {
			recipient = strings.TrimSpace(row[0])
		}
		out = append(out, CampaignRecord{
			WorkspaceID: workspaceID,
			CampaignID:  campaignID,
			Recipient:   recipient,
			Attributes:  attrs,
		})
	}
	return out, nil
}
