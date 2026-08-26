package chatpipeline

import (
	"strings"
	"testing"
)

func TestDataAnalysisEvidenceInstructionBalancesSQLAndRetrieval(t *testing.T) {
	if strings.Contains(dataAnalysisEvidenceInstruction, "优先依据") {
		t.Fatal("ordinary fact queries must not force the model to prefer SQL over retrieval evidence")
	}
	for _, required := range []string{"交叉核对", "统计", "SQL 结果为准", "不得因 SQL 未命中而忽略其他证据"} {
		if !strings.Contains(dataAnalysisEvidenceInstruction, required) {
			t.Fatalf("missing evidence fusion instruction %q", required)
		}
	}
}
