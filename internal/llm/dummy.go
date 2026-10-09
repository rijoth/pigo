package llm

import "context"

type dummyProvider struct{}

// NewDummy returns a provider that answers without calling a model. It lets the
// UI run when no model is configured.
func NewDummy() Provider { return dummyProvider{} }

func (dummyProvider) Stream(_ context.Context, _ Request) (<-chan Delta, error) {
	ch := make(chan Delta, 1)
	ch <- Delta{Text: "no model configured; set PIGO_MODEL or pass -m provider/model"}
	close(ch)
	return ch, nil
}
