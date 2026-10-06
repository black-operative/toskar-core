package guide

import "testing"

// A question about the GPU is answered from the guide's GPU section (#317).
func TestAboutFindsTheGPUSection(t *testing.T) {
	for _, q := range []string{"Is my model using the GPU?", "Why does Toskar say CPU only?", "How do I know my GPU is being used in Toskar?"} {
		found := false
		for _, p := range About(q) {
			if p.Section == "Is my model using the GPU?" {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: the GPU section wasn't found in %+v", q, About(q))
		}
	}
}
