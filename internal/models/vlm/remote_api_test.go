package vlm

import (
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestFormatOpenAIRequestErrorWithoutInnerError(t *testing.T) {
	err := formatOpenAIError(&openai.RequestError{
		HTTPStatusCode: 400,
		HTTPStatus:     "400 Bad Request",
		Body:           []byte(`{"code":20041,"message":"The model is not a VLM"}`),
	})
	if strings.Contains(err.Error(), "%!s") {
		t.Fatalf("malformed error: %s", err)
	}
	if !strings.Contains(err.Error(), "The model is not a VLM") {
		t.Fatalf("missing response body: %s", err)
	}
}
