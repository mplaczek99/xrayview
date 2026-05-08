package httpapi

import (
	"testing"

	"xrayview/backend/internal/contracts"
)

var benchmarkSSEFrameSink []byte

func BenchmarkSSEHubBroadcast(b *testing.B) {
	snapshot := contracts.JobSnapshot{
		JobID:   "benchmark-job-1234567890",
		JobKind: contracts.JobKindProcessStudy,
		State:   contracts.JobStateRunning,
		Progress: contracts.JobProgress{
			Percent: 67,
			Stage:   "processing",
			Message: "Writing processed preview",
		},
		FromCache: true,
	}

	b.Run("no_clients", func(b *testing.B) {
		hub := newSSEHub()
		b.ReportAllocs()

		for b.Loop() {
			hub.broadcast(snapshot)
		}
	})

	b.Run("8_clients", func(b *testing.B) {
		hub := newSSEHub()
		clients := make([]chan []byte, 8)
		for i := range clients {
			clients[i] = hub.subscribe()
		}
		b.ReportAllocs()

		for b.Loop() {
			hub.broadcast(snapshot)
			for _, ch := range clients {
				benchmarkSSEFrameSink = <-ch
			}
		}
	})
}
