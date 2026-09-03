package handler

import (
	"errors"
	"strings"
	"testing"
)

func TestVisionCapabilityTestMessageForNonVLM(t *testing.T) {
	message := visionCapabilityTestMessage(errors.New("The model is not a VLM (Vision Language Model)."))
	if message != "该模型不支持视觉/多模态输入" {
		t.Fatalf("unexpected message: %s", message)
	}
}

func TestVisionCapabilityTestMessagePreservesOtherFailures(t *testing.T) {
	message := visionCapabilityTestMessage(errors.New("unauthorized"))
	if !strings.Contains(message, "unauthorized") {
		t.Fatalf("unexpected message: %s", message)
	}
}
