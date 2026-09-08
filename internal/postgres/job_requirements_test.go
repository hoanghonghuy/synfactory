package postgres

import (
	"encoding/json"
	"testing"

	"github.com/hoanghonghuy/synfactory/internal/domain"
)

func TestMetadataWithRequirementsHandlesJSONNull(t *testing.T) {
	metadata, err := metadataWithRequirements(json.RawMessage(`null`), domain.JobRequirements{Docker: true})
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &values); err != nil {
		t.Fatal(err)
	}
	if string(values["requirements"]) != `{"docker":true}` {
		t.Fatalf("unexpected requirements metadata: %s", values["requirements"])
	}
}
