package handler

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVisionCountAnswerRequiresObservedCount(t *testing.T) {
	if err := validateVisionCountAnswer(" 6 ", 6); err != nil {
		t.Fatalf("expected exact count to pass: %v", err)
	}
	for _, output := range []string{"", "图片中有6个", "5", "red circles"} {
		if err := validateVisionCountAnswer(output, 6); err == nil {
			t.Fatalf("expected %q to fail", output)
		}
	}
}

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
