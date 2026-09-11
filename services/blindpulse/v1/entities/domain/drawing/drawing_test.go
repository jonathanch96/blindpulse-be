package drawing

import "testing"

// This file is the BR-01 guard for the one payload a client controls end to end. A drawing goes in
// as opaque bytes and comes back out of a pre-reveal endpoint as the same bytes, so anything that
// could date the window has to be refused on the way in — there is no second chance on the way out.

func TestDateLeakRefusesWhatCouldDateTheWindow(t *testing.T) {
	leaks := map[string]string{
		"an ISO date in a value":        `{"anchors":[{"index":4,"price":"1.07","d":"2023-03-14"}]}`,
		"a slashed date":                `{"anchors":[{"index":4,"label2":"2023/03/14"}]}`,
		"a field that names a clock":    `{"anchors":[{"index":4,"timestamp":0}]}`,
		"a camelCase clock field":       `{"drawnAt":"whenever"}`,
		"unix seconds as a number":      `{"anchors":[{"index":4,"x":1678780800}]}`,
		"unix milliseconds as a number": `{"anchors":[{"index":4,"x":1678780800000}]}`,
		"nested inside an array":        `{"points":[[1,2],[3,"2019-12-31"]]}`,
		"a clock field nested deep":     `{"meta":{"style":{"created_at":null}}}`,
	}
	for name, payload := range leaks {
		if !DateLeak([]byte(payload)) {
			t.Errorf("%s was accepted: %s", name, payload)
		}
	}
}

// The other half of the guard, and the half that makes it usable. A check that refuses ordinary
// drawings gets turned off by the first person who hits it.
func TestDateLeakAcceptsAnOrdinaryDrawing(t *testing.T) {
	clean := map[string]string{
		"a trendline":         `{"anchors":[{"index":140,"price":"1.07231"},{"index":214,"price":"1.08004"}]}`,
		"a shaded zone":       `{"anchors":[{"index":140,"price":"1.07"},{"index":214,"price":"1.08"}],"opacity":0.25}`,
		"a fib with levels":   `{"anchors":[{"index":1,"price":"1.0"},{"index":9,"price":"2.0"}],"levels":[0,0.236,0.382,0.5,0.618,0.786,1]}`,
		"a big bar index":     `{"anchors":[{"index":999999,"price":"1.07"}]}`,
		"a long brush stroke": `{"anchors":[{"index":1,"price":"1.1"},{"index":2,"price":"1.2"},{"index":3,"price":"1.3"}]}`,
	}
	for name, payload := range clean {
		if DateLeak([]byte(payload)) {
			t.Errorf("%s was refused: %s", name, payload)
		}
	}
}

// The exemption that keeps the guard honest. A trader who writes "SVB week, 2023-03-14" in their own
// note is guessing — the server told them nothing — and refusing their note would be the product
// policing the trader's own conclusions instead of its own payloads.
func TestDateLeakDoesNotPoliceWhatTheTraderTyped(t *testing.T) {
	prose := []string{
		`{"anchors":[{"index":4,"price":"1.07"}],"text":"looks like 2023-03-14, SVB week?"}`,
		`{"anchors":[{"index":4,"price":"1.07"}],"label":"2008/09/15 vibes"}`,
		`{"anchors":[{"index":4,"price":"1.07"}],"note":"1678780800 if I had to guess"}`,
	}
	for _, payload := range prose {
		if DateLeak([]byte(payload)) {
			t.Errorf("a trader's own prose was refused: %s", payload)
		}
	}
	// The exemption is scoped to the prose field and does not spread to its siblings.
	withSibling := `{"text":"nothing to see","anchors":[{"index":4,"when":"2023-03-14"}]}`
	if !DateLeak([]byte(withSibling)) {
		t.Errorf("the prose exemption leaked to a sibling field: %s", withSibling)
	}
}

func TestKindVocabularyCoversTheTerminalsTools(t *testing.T) {
	// The eleven tools the terminal ships. The database used to hold this list and allowed seven of
	// them, so a ray, a polyline or a brush stroke could not be saved at all — this asserts the
	// vocabulary and the toolkit have not drifted apart again.
	for _, kind := range []Kind{
		KindTrendline, KindHorizontal, KindRay, KindExtended, KindVertical,
		KindFibRetracement, KindFibExtension, KindZone, KindPolyline, KindBrush, KindNote,
	} {
		if !kind.Valid() {
			t.Errorf("the terminal ships %q and the server does not accept it", kind)
		}
	}
	for _, kind := range []Kind{"", "supply_zone", "Trendline", "arbitrary"} {
		if kind.Valid() {
			t.Errorf("%q is accepted and should not be", kind)
		}
	}
}
