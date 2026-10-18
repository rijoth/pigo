package tools

import (
	"context"
	"testing"
)

func TestRecallSpec(t *testing.T) {
	spec := Recall{}.Spec()
	if spec.Name != "pigo_recall" {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.Description == "" || spec.Schema == nil {
		t.Fatalf("incomplete spec: %+v", spec)
	}
	if _, err := (Recall{}).Run(context.Background(), nil); err == nil {
		t.Fatal("expected error when Fn is nil")
	}
}
