package types

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yanyiwu/gojieba"
)

func TestNewJiebaUsesConfiguredDictionaryDirectory(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{
		"jieba.dict.utf8",
		"hmm_model.utf8",
		"user.dict.utf8",
		"idf.utf8",
		"stop_words.utf8",
	} {
		data, err := os.ReadFile(filepath.Join(gojieba.DICT_DIR, name))
		if err != nil {
			t.Skipf("gojieba dictionary is unavailable: %v", err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("WEKNORA_JIEBA_DICT_DIR", directory)
	jieba := newJieba()
	if got := jieba.Cut("离线词典回归", false); len(got) == 0 {
		t.Fatal("configured jieba dictionary returned no tokens")
	}
}
