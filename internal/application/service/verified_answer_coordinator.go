package service

import "github.com/Tencent/WeKnora/internal/types"

// VerifiedAnswerCoordinator is kept as a service-package alias for callers
// that used the first implementation location. The state machine lives in
// types so the chat pipeline and service tests share one contract.
type VerifiedAnswerCoordinator = types.VerifiedAnswerCoordinator

func NewVerifiedAnswerCoordinator(config types.VerifiedAnswerConfig) *types.VerifiedAnswerCoordinator {
	return types.NewVerifiedAnswerCoordinator(config)
}
