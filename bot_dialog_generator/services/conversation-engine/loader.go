package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/awsplatform"
	botdialoggeneratorv1 "github.com/Nggerizo97/Dialog_Bot_Creation/bot_dialog_generator/libs/go/botdef/bot_dialog_generator/v1"
	"google.golang.org/protobuf/proto"
)

// DefinitionLoader loads compiled and KMS-verified BotDefinition artifacts from S3
// and caches them by (workspaceID, botID, version), migrating load_pickle and KMS
// verification from F:\artemis\bot_engine\src\utils.py.
type DefinitionLoader struct {
	bucket string
	s3     *awsplatform.S3Service
	sqs    *awsplatform.SQSService
	queue  string

	mu    sync.RWMutex
	cache map[string]*botdialoggeneratorv1.BotDefinition
}

// NewDefinitionLoader creates a DefinitionLoader backed by S3 (with KMS verification)
// and an optional SQS event queue.
func NewDefinitionLoader(bucket string, s3Svc *awsplatform.S3Service, sqsSvc *awsplatform.SQSService, eventsQueueURL string) *DefinitionLoader {
	return &DefinitionLoader{
		bucket: bucket,
		s3:     s3Svc,
		sqs:    sqsSvc,
		queue:  eventsQueueURL,
		cache:  make(map[string]*botdialoggeneratorv1.BotDefinition),
	}
}

func cacheKey(workspaceID, botID, version string) string {
	return workspaceID + "/" + botID + "/" + version
}

// Load retrieves a verified BotDefinition for (workspaceID, botID, version), fetching
// from S3 on cache miss and enforcing workspace isolation (ADR 0003).
func (l *DefinitionLoader) Load(ctx context.Context, workspaceID, botID, version string) (*botdialoggeneratorv1.BotDefinition, error) {
	key := cacheKey(workspaceID, botID, version)
	l.mu.RLock()
	if def, ok := l.cache[key]; ok {
		l.mu.RUnlock()
		return def, nil
	}
	l.mu.RUnlock()

	stored, err := l.s3.GetDefinition(ctx, l.bucket, workspaceID, botID, version, "")
	if err != nil {
		return nil, err
	}
	var def botdialoggeneratorv1.BotDefinition
	if err := proto.Unmarshal(stored.Bytes, &def); err != nil {
		return nil, fmt.Errorf("unmarshal BotDefinition: %w", err)
	}
	if def.WorkspaceId != workspaceID {
		return nil, fmt.Errorf("%w: artifact belongs to %q, requested %q", ErrWorkspaceMismatch, def.WorkspaceId, workspaceID)
	}

	l.mu.Lock()
	l.cache[key] = &def
	l.mu.Unlock()
	return &def, nil
}

// Invalidate removes cached definitions for a bot when a new version is published.
func (l *DefinitionLoader) Invalidate(workspaceID, botID, version string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.cache, cacheKey(workspaceID, botID, version))
}

// EmitTurnEvent publishes a conversation step event to SQS (migrating
// F:\artemis\bot_engine\src\libs\sqs.py SQS.put).
func (l *DefinitionLoader) EmitTurnEvent(ctx context.Context, workspaceID string, event any) (string, error) {
	if l.sqs == nil || l.queue == "" {
		return "", nil
	}
	return l.sqs.SendJSON(ctx, l.queue, workspaceID, event)
}
