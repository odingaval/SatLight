package bounty

import "testing"

func TestCanCancelBounty(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{state: "open", want: true},
		{state: "claimed", want: false},
		{state: "in_review", want: false},
		{state: "draft", want: false},
		{state: "cancelled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := canCancelBounty(tt.state); got != tt.want {
				t.Errorf("canCancelBounty(%q) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}
