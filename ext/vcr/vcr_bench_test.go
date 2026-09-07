package vcr

import (
	"fmt"
	"net/http"
	"testing"
)

// BenchmarkPlaybackRoundTrip measures the per-request cassette lookup cost
// in ModePlayback - the vcr hot path (playbackRoundTrip runs on every
// request made through a VCR-wrapped client in playback mode).
//
// ModeRecord is not benchmarked: recording is dominated by a real network
// round trip plus a debounced disk write, not by vcr's own per-request
// logic, so it is not a hot path worth micro-benchmarking here.
func BenchmarkPlaybackRoundTrip(b *testing.B) {
	const n = 1000

	interactions := make([]Interaction, n)
	reqs := make([]*http.Request, n)
	for i := 0; i < n; i++ {
		url := fmt.Sprintf("http://bench.test/item/%d", i)
		interactions[i] = Interaction{
			Request: RecordedRequest{Method: http.MethodGet, URL: url},
			Response: RecordedResponse{
				Status: http.StatusOK,
				Header: map[string][]string{"Content-Type": {"application/json"}},
				Body:   `{"ok":true}`,
			},
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			b.Fatalf("build request: %v", err)
		}
		reqs[i] = req
	}

	v := &VCR{
		mode:     ModePlayback,
		cassette: &Cassette{Interactions: interactions},
	}
	tr := &vcrTransport{vcr: v}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if i%n == 0 {
			v.mu.Lock()
			v.playbackIdx = 0
			v.mu.Unlock()
		}

		resp, err := tr.playbackRoundTrip(reqs[i%n])
		if err != nil {
			b.Fatalf("playbackRoundTrip: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}
