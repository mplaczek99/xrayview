package contracts

import (
	"encoding/json"
	"testing"
)

func TestDefaultProcessingManifestMatchesContractPayload(t *testing.T) {
	payload, err := json.Marshal(DefaultProcessingManifest())
	if err != nil {
		t.Fatal(err)
	}

	expected := `{"defaultPresetId":"default","presets":[{"id":"default","controls":{"brightness":0,"contrast":1,"invert":false,"equalize":false,"palette":"none"}},{"id":"xray","controls":{"brightness":10,"contrast":1.4,"invert":false,"equalize":true,"palette":"bone"}},{"id":"high-contrast","controls":{"brightness":0,"contrast":1.8,"invert":false,"equalize":true,"palette":"none"}}]}`
	if string(payload) != expected {
		t.Fatalf("manifest JSON = %s", payload)
	}
}
