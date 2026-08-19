package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type duplicateModelRepository struct {
	models      []*types.Model
	createCalls int
	listErr     error
}

func (r *duplicateModelRepository) Create(_ context.Context, _ *types.Model) error {
	r.createCalls++
	return nil
}

func (r *duplicateModelRepository) GetByID(_ context.Context, _ uint64, _ string) (*types.Model, error) {
	return nil, nil
}

func (r *duplicateModelRepository) List(
	_ context.Context, _ uint64, modelType types.ModelType, source types.ModelSource,
) ([]*types.Model, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	result := make([]*types.Model, 0, len(r.models))
	for _, model := range r.models {
		if (modelType == "" || model.Type == modelType) && (source == "" || model.Source == source) {
			result = append(result, model)
		}
	}
	return result, nil
}

func (r *duplicateModelRepository) Update(_ context.Context, _ *types.Model) error     { return nil }
func (r *duplicateModelRepository) Delete(_ context.Context, _ uint64, _ string) error { return nil }
func (r *duplicateModelRepository) ClearDefaultByType(
	_ context.Context, _ uint, _ types.ModelType, _ string,
) error {
	return nil
}

func TestCreateModelReusesEquivalentRegistration(t *testing.T) {
	repo := &duplicateModelRepository{models: []*types.Model{{
		ID:     "existing",
		Name:   "Qwen/Qwen3.6-27B",
		Type:   types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			Provider: "SiliconFlow",
			BaseURL:  "https://api.siliconflow.cn/v1/",
		},
	}}}
	service := &modelService{repo: repo}
	ctx := context.WithValue(context.Background(), types.UserContextKey, &types.User{
		Role: types.UserRolePlatformAdmin,
	})
	candidate := &types.Model{
		Name:   " Qwen/Qwen3.6-27B ",
		Type:   types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			BaseURL: "https://api.siliconflow.cn/v1",
		},
	}

	if err := service.CreateModel(ctx, candidate); err != nil {
		t.Fatalf("CreateModel() error = %v, want nil", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("repository Create() called %d times, want 0", repo.createCalls)
	}
	if candidate.ID != "existing" {
		t.Fatalf("CreateModel() reused ID = %q, want existing", candidate.ID)
	}
}

func TestSameModelRegistrationAllowsDifferentRole(t *testing.T) {
	chatModel := &types.Model{
		Name:   "Qwen/Qwen3.6-27B",
		Type:   types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			Provider: "siliconflow",
			BaseURL:  "https://api.siliconflow.cn/v1",
		},
	}
	vlmModel := *chatModel
	vlmModel.Type = types.ModelTypeVLLM

	if sameModelRegistration(chatModel, &vlmModel) {
		t.Fatal("models with different roles were treated as duplicate registrations")
	}
}

func TestUpdateModelRejectsEquivalentRegistrationAfterSkippingItself(t *testing.T) {
	own := &types.Model{
		ID:     "own",
		Name:   "Qwen/Qwen3.6-27B",
		Type:   types.ModelTypeKnowledgeQA,
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			Provider: "siliconflow",
			BaseURL:  "https://api.siliconflow.cn/v1",
		},
	}
	duplicate := *own
	duplicate.ID = "duplicate"
	repo := &duplicateModelRepository{models: []*types.Model{own, &duplicate}}
	service := &modelService{repo: repo}
	ctx := context.WithValue(context.Background(), types.UserContextKey, &types.User{
		Role: types.UserRolePlatformAdmin,
	})
	ctx = context.WithValue(ctx, types.TenantIDContextKey, uint64(1))

	err := service.UpdateModel(ctx, own)
	if !errors.Is(err, ErrModelAlreadyExists) {
		t.Fatalf("UpdateModel() error = %v, want ErrModelAlreadyExists", err)
	}
}
