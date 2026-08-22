package consumer

import "testing"

func TestParseLSNParsesPostgreSQLPosition(t *testing.T) {
	got, err := parseLSN("16/B374D848")
	if err != nil {
		t.Fatalf("parseLSN returned an error: %v", err)
	}

	const want uint64 = 0x16B374D848
	if got != want {
		t.Fatalf("parseLSN returned %X, want %X", got, want)
	}
}
