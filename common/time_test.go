package common

import (
	"testing"
	"time"
)

func TestFormatDisplayTimeConvertsUTCToBeijing(t *testing.T) {
	// 数据库读回的时间带 UTC Location：06:25:36Z 对应北京时间 14:25:36。
	utc := time.Date(2026, 10, 25, 6, 25, 36, 0, time.UTC)
	if got, want := FormatDisplayTime(utc), "2026-10-25 14:25:36"; got != want {
		t.Fatalf("FormatDisplayTime(%v) = %q, want %q", utc, got, want)
	}
}

func TestFormatDisplayTimeHandlesBeijingInput(t *testing.T) {
	// 已经带 +08:00 的时间不应被再次偏移。
	cst := time.Date(2026, 10, 25, 14, 25, 36, 0, time.FixedZone("CST", 8*60*60))
	if got, want := FormatDisplayTime(cst), "2026-10-25 14:25:36"; got != want {
		t.Fatalf("FormatDisplayTime(%v) = %q, want %q", cst, got, want)
	}
}
