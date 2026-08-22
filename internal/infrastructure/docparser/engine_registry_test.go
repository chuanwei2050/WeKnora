package docparser

import "testing"

func TestSelectConfiguredEngineUsesPlatformConfiguration(t *testing.T) {
	tests := []struct {
		name                string
		fileType            string
		docreaderConnected  bool
		hasCloudCredentials bool
		overrides           map[string]string
		want                string
	}{
		{name: "simple formats stay local", fileType: "txt", want: SimpleEngineName},
		{name: "connected builtin handles pdf", fileType: "pdf", docreaderConnected: true, want: "builtin"},
		{name: "ppt falls through to platform MinerU", fileType: ".PPTX", overrides: map[string]string{"mineru_endpoint": "http://mineru"}, want: "mineru"},
		{name: "cloud credentials handle ppt before MinerU", fileType: "ppt", hasCloudCredentials: true, overrides: map[string]string{"mineru_endpoint": "http://mineru"}, want: WeKnoraCloudEngineName},
		{name: "unconfigured complex type has no engine", fileType: "pptx", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := SelectConfiguredEngine(test.fileType, test.docreaderConnected, test.hasCloudCredentials, test.overrides)
			if got != test.want {
				t.Fatalf("SelectConfiguredEngine() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestListAllEnginesIncludesMarkitdownWhenDocReaderConnected(t *testing.T) {
	engines := ListAllEngines(true, nil, nil)
	for _, e := range engines {
		if e.Name != "markitdown" {
			continue
		}
		if !e.Available {
			t.Fatalf("markitdown should be available when docreader is connected, reason: %q", e.UnavailableReason)
		}
		return
	}
	t.Fatal("markitdown engine not found in ListAllEngines output")
}

func TestListAllEnginesMarkitdownUnavailableWithoutDocReader(t *testing.T) {
	engines := ListAllEngines(false, nil, nil)
	for _, e := range engines {
		if e.Name != "markitdown" {
			continue
		}
		if e.Available {
			t.Fatal("markitdown should be unavailable when docreader is disconnected")
		}
		return
	}
	t.Fatal("markitdown engine not found in ListAllEngines output")
}
