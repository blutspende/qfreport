package render

import "testing"

func TestEmbeddedTemplateSchemaCompiles(t *testing.T) {
	if templateSchemaErr != nil {
		t.Fatalf("compile embedded report schema: %v", templateSchemaErr)
	}
}
