package proc

import "testing"

func BenchmarkScan(b *testing.B) {
	s := NewSampler()
	for i := 0; i < b.N; i++ {
		s.Scan(16000000)
	}
}
