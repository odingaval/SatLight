package notify

import "testing"

func TestParseSMTPAddr(t *testing.T) {
	addr, err := parseSMTPAddr("smtp://localhost:1025")
	if err != nil || addr != "localhost:1025" {
		t.Fatalf("parseSMTPAddr = %q %v", addr, err)
	}
	for _, bad := range []string{"", "localhost", "smtp://", "smtp://host-without-port"} {
		if _, err := parseSMTPAddr(bad); err == nil {
			t.Errorf("parseSMTPAddr(%q) err = nil, want error", bad)
		}
	}
}
