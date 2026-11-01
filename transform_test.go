package webpi

import (
	"math/rand/v2"
	"testing"
)

func Test_fdct4_thenIdct4Add_recoversResidualWithinOne(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for trial := range 2000 {
		var res [16]int32
		var pred [16]uint8
		for i := range 16 {
			pred[i] = 128
			res[i] = int32(r.IntN(255) - 127)
		}
		c := fdct4(&res)
		got := idct4Add(&c, &pred)
		for i := range 16 {
			want := int32(pred[i]) + res[i]
			if d := int32(got[i]) - want; d < -1 || d > 1 {
				t.Fatalf("trial %d pos %d: got %d want %d", trial, i, got[i], want)
			}
		}
	}
}

func Test_fwht_thenIwht_recoversDCWithinOne(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for trial := range 2000 {
		var dc [16]int32
		for i := range 16 {
			dc[i] = int32(r.IntN(4000) - 2000)
		}
		y2 := fwht(&dc)
		got := iwht(&y2)
		for i := range 16 {
			if d := got[i] - dc[i]; d < -1 || d > 1 {
				t.Fatalf("trial %d pos %d: got %d want %d", trial, i, got[i], dc[i])
			}
		}
	}
}
