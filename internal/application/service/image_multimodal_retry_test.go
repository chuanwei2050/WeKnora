package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/hibiken/asynq"
)

type imageFinalizeEnqueuer struct {
	tasks []*asynq.Task
}

func (e *imageFinalizeEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.tasks = append(e.tasks, task)
	return &asynq.TaskInfo{ID: "post-process"}, nil
}

func TestImageVLMFailureRetriesBeforeLastAttempt(t *testing.T) {
	enqueuer := &imageFinalizeEnqueuer{}
	service := &ImageMultimodalService{taskEnqueuer: enqueuer}
	payload := types.ImageMultimodalPayload{KnowledgeID: "knowledge", ImageURL: "image.png"}

	err := service.handleVLMFailure(context.Background(), payload, "OCR", errors.New("vlm unavailable"), false)
	if err == nil {
		t.Fatal("expected retryable error before the last attempt")
	}
	if len(enqueuer.tasks) != 0 {
		t.Fatal("post-processing must not start before retries are exhausted")
	}
}

func TestImageVLMFailureFinalizesAfterLastAttempt(t *testing.T) {
	enqueuer := &imageFinalizeEnqueuer{}
	service := &ImageMultimodalService{taskEnqueuer: enqueuer}
	payload := types.ImageMultimodalPayload{
		TenantID:        1,
		KnowledgeID:     "knowledge",
		KnowledgeBaseID: "kb",
		ImageURL:        "image.png",
	}

	if err := service.handleVLMFailure(context.Background(), payload, "OCR", errors.New("vlm unavailable"), true); err != nil {
		t.Fatal(err)
	}
	if len(enqueuer.tasks) != 1 || enqueuer.tasks[0].Type() != types.TypeKnowledgePostProcess {
		t.Fatalf("expected one knowledge post-process task, got %+v", enqueuer.tasks)
	}
}
