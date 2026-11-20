// naldump — diagnostika: MediaMTX'dagi nashr yo'lini (agent publish qilgan
// oqimni) o'qib, real NAL turlarini sanaydi. H.265 zanjirida qaysi tomon
// noto'g'ri paketlashini aniqlash uchun ishlatilgan (2026-11-21).
//
//	go run ./cmd/naldump "rtsp://127.0.0.1:8554/cam/<agent>/<cam>?token=<read>"
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"central-vms/client/internal/cams"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	seen := map[int]int{}
	first := []int{}
	n := 0
	err := cams.Pull(ctx, cams.PullOptions{URL: os.Args[1]}, func(au cams.AU) error {
		n++
		for _, nal := range au.Nals {
			if len(nal) < 2 {
				continue
			}
			t := int((nal[0] >> 1) & 0x3f)
			seen[t]++
			if len(first) < 12 {
				first = append(first, t)
			}
		}
		if n >= 60 {
			return fmt.Errorf("yetarli")
		}
		return nil
	})
	fmt.Printf("  AU=%d birinchi NAL turlari=%v\n", n, first)
	fmt.Printf("  turlar soni: %v (err=%v)\n", seen, err)
}
