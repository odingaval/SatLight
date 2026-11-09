package bounty

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

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

func TestCalculatePlatformFee(t *testing.T) {
	tests := []struct {
		name       string
		rewardSats int64
		feeBPS     int
		want       int64
	}{
		{name: "two percent", rewardSats: 10000, feeBPS: 200, want: 200},
		{name: "round down", rewardSats: 501, feeBPS: 200, want: 10},
		{name: "minimum one sat", rewardSats: 1, feeBPS: 1, want: 1},
		{name: "zero fee configuration", rewardSats: 1000, feeBPS: 0, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculatePlatformFee(tt.rewardSats, tt.feeBPS); got != tt.want {
				t.Errorf("calculatePlatformFee(%d, %d) = %d, want %d", tt.rewardSats, tt.feeBPS, got, tt.want)
			}
		})
	}
}

func TestCanExpireBounty(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		state     string
		expiresAt time.Time
		want      bool
	}{
		{name: "open and expired", state: "open", expiresAt: now.Add(-time.Second), want: true},
		{name: "open at expiry", state: "open", expiresAt: now, want: true},
		{name: "open before expiry", state: "open", expiresAt: now.Add(time.Second), want: false},
		{name: "claimed", state: "claimed", expiresAt: now.Add(-time.Second), want: false},
		{name: "cancelled", state: "cancelled", expiresAt: now.Add(-time.Second), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canExpireBounty(tt.state, tt.expiresAt, now); got != tt.want {
				t.Errorf("canExpireBounty(%q, %v, %v) = %t, want %t", tt.state, tt.expiresAt, now, got, tt.want)
			}
		})
	}
}

func TestBuildListBountiesQueryPlacesCursorBeforeOrderAndLimit(t *testing.T) {
	cursorTime := time.Date(2026, 10, 23, 12, 30, 0, 0, time.UTC)
	cursor := encodeBountyCursor(cursorTime, 42)
	query, args, limit, err := buildListBountiesQuery(ListFilters{
		Query:      "bug",
		Difficulty: "medium",
		MinReward:  1000,
		MaxReward:  30000,
		Cursor:     cursor,
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("buildListBountiesQuery() error = %v", err)
	}

	cursorIndex := strings.Index(query, "AND (b.created_at, b.id) < ($5::timestamptz, $6::bigint)")
	orderIndex := strings.Index(query, "ORDER BY b.created_at DESC, b.id DESC")
	limitIndex := strings.Index(query, "LIMIT $7")
	if cursorIndex < 0 || orderIndex <= cursorIndex || limitIndex <= orderIndex {
		t.Fatalf("cursor, ordering, and limit clauses are misplaced in query:\n%s", query)
	}

	wantArgs := []interface{}{"%bug%", "medium", int64(1000), int64(30000), cursorTime, int64(42), 11}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("query args = %#v, want %#v", args, wantArgs)
	}
	if limit != 10 {
		t.Errorf("limit = %d, want 10", limit)
	}
}

func TestCanDisputeBounty(t *testing.T) {
	tests := []struct {
		state string
		want  bool
	}{
		{state: "open", want: true},
		{state: "claimed", want: true},
		{state: "in_review", want: true},
		{state: "draft", want: false},
		{state: "merged", want: false},
		{state: "paid", want: false},
		{state: "payout_failed", want: false},
		{state: "expired", want: false},
		{state: "cancelled", want: false},
		{state: "disputed", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got := canDisputeBounty(tt.state); got != tt.want {
				t.Errorf("canDisputeBounty(%q) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}

func TestValidBountyTransition(t *testing.T) {
	valid := [][2]string{
		{"draft", "open"},
		{"open", "claimed"}, {"open", "cancelled"}, {"open", "expired"}, {"open", "disputed"},
		{"claimed", "open"}, {"claimed", "in_review"}, {"claimed", "merged"}, {"claimed", "expired"}, {"claimed", "disputed"},
		{"in_review", "claimed"}, {"in_review", "merged"}, {"in_review", "disputed"},
		{"merged", "paid"}, {"merged", "payout_failed"},
		{"payout_failed", "merged"},
		{"disputed", "open"}, {"disputed", "claimed"}, {"disputed", "in_review"}, {"disputed", "merged"}, {"disputed", "cancelled"},
	}
	for _, tt := range valid {
		if !validBountyTransition(tt[0], tt[1]) {
			t.Errorf("validBountyTransition(%q, %q) = false, want true", tt[0], tt[1])
		}
	}
	invalid := [][2]string{
		{"draft", "claimed"}, {"draft", "paid"},
		{"open", "open"}, {"open", "paid"}, {"open", "merged"},
		{"claimed", "paid"}, {"claimed", "cancelled"},
		{"in_review", "open"}, {"in_review", "paid"},
		{"merged", "open"}, {"merged", "cancelled"}, {"merged", "disputed"},
		{"payout_failed", "paid"}, {"payout_failed", "open"},
		{"paid", "open"}, {"paid", "merged"},
		{"expired", "open"}, {"cancelled", "open"},
		{"disputed", "paid"}, {"disputed", "expired"},
		{"", "open"}, {"open", ""},
	}
	for _, tt := range invalid {
		if validBountyTransition(tt[0], tt[1]) {
			t.Errorf("validBountyTransition(%q, %q) = true, want false", tt[0], tt[1])
		}
	}
}

func TestValidDisputeResolution(t *testing.T) {
	for _, valid := range []string{"resolved_pay", "resolved_cancel", "resolved_other"} {
		if !validDisputeResolution(valid) {
			t.Errorf("validDisputeResolution(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{"", "open", "resolved", "RESOLVED_PAY"} {
		if validDisputeResolution(invalid) {
			t.Errorf("validDisputeResolution(%q) = true, want false", invalid)
		}
	}
}

func TestBountyCursorRoundTripAndValidation(t *testing.T) {
	createdAt := time.Date(2026, 10, 23, 12, 30, 0, 123456789, time.FixedZone("EAT", 3*60*60))
	wantTime := createdAt.UTC()
	cursor := encodeBountyCursor(createdAt, 99)
	gotTime, gotID, err := parseBountyCursor(cursor)
	if err != nil {
		t.Fatalf("parseBountyCursor() error = %v", err)
	}
	if !gotTime.Equal(wantTime) || gotID != 99 {
		t.Errorf("parseBountyCursor() = (%v, %d), want (%v, 99)", gotTime, gotID, wantTime)
	}

	for _, invalid := range []string{"", "not-a-time|2", "2026-10-23T12:30:00Z|0", "2026-10-23T12:30:00Z|x"} {
		if _, _, err := parseBountyCursor(invalid); err == nil {
			t.Errorf("parseBountyCursor(%q) error = nil, want error", invalid)
		}
	}
}
