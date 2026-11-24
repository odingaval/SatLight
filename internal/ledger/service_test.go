package ledger

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"testing"
)

func TestValidateEntriesAcceptsBalancedTransaction(t *testing.T) {
	err := validateEntries([]Entry{
		{Account: "project:1:available", Direction: "debit", AmountSats: 1500},
		{Account: "project:1:reserved", Direction: "credit", AmountSats: 1500},
	})
	if err != nil {
		t.Fatalf("validateEntries() error = %v", err)
	}
}

// FuzzValidateEntries proves the ledger gate never panics and accepts exactly
// the balanced transactions: at least two entries, positive amounts,
// debit/credit directions only, equal sides, no int64 overflow. The oracle
// sums with big.Int so it cannot share the implementation's overflow bug.
func FuzzValidateEntries(f *testing.F) {
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 0, 3, 232, 1, 0, 0, 0, 0, 0, 0, 3, 232})
	f.Add([]byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 100, 1, 0, 0, 0, 0, 0, 0, 0, 99})
	f.Add([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 10 || len(data) > 200 {
			return
		}
		count := int(data[0]%8) + 1
		if 1+count*9 > len(data) {
			return
		}
		var entries []Entry
		for i := 0; i < count; i++ {
			raw := data[1+i*9 : 1+(i+1)*9]
			var direction string
			switch raw[0] % 3 {
			case 0:
				direction = "debit"
			case 1:
				direction = "credit"
			default:
				direction = "transfer"
			}
			entries = append(entries, Entry{Direction: direction, AmountSats: int64(binary.BigEndian.Uint64(raw[1:]))})
		}
		err := validateEntries(entries)

		wantOK := len(entries) >= 2
		debits, credits := big.NewInt(0), big.NewInt(0)
		for _, e := range entries {
			if e.AmountSats <= 0 || (e.Direction != "debit" && e.Direction != "credit") {
				wantOK = false
			}
			if e.Direction == "debit" {
				debits.Add(debits, big.NewInt(e.AmountSats))
			} else if e.Direction == "credit" {
				credits.Add(credits, big.NewInt(e.AmountSats))
			}
		}
		maxInt := big.NewInt(math.MaxInt64)
		if debits.Cmp(maxInt) > 0 || credits.Cmp(maxInt) > 0 || debits.Cmp(credits) != 0 {
			wantOK = false
		}
		// Negative-amount entries poison big.Int sums upward; the loop above
		// already failed those via the positivity check.
		if (err == nil) != wantOK {
			t.Fatalf("validateEntries(%v) err = %v, want ok=%v", entries, err, wantOK)
		}
	})
}

func TestProjectAccountUsesDecimalIDs(t *testing.T) {
	if got, want := projectAccount(1024, "available"), "project:1024:available"; got != want {
		t.Fatalf("projectAccount() = %q, want %q", got, want)
	}
}

func TestValidateEntriesRejectsInvalidTransactions(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []Entry
		wantErr error
	}{
		{
			name:    "too few entries",
			entries: []Entry{{Direction: "debit", AmountSats: 1}},
			wantErr: ErrTooFewEntries,
		},
		{
			name: "unbalanced",
			entries: []Entry{
				{Direction: "debit", AmountSats: 100},
				{Direction: "credit", AmountSats: 99},
			},
			wantErr: ErrLedgerImbalance,
		},
		{
			name: "invalid direction",
			entries: []Entry{
				{Direction: "transfer", AmountSats: 100},
				{Direction: "credit", AmountSats: 100},
			},
			wantErr: ErrInvalidLedgerEntry,
		},
		{
			name: "zero amount",
			entries: []Entry{
				{Direction: "debit", AmountSats: 0},
				{Direction: "credit", AmountSats: 0},
			},
			wantErr: ErrInvalidLedgerEntry,
		},
		{
			name: "negative amount",
			entries: []Entry{
				{Direction: "debit", AmountSats: -100},
				{Direction: "credit", AmountSats: -100},
			},
			wantErr: ErrInvalidLedgerEntry,
		},
		{
			name: "overflow",
			entries: []Entry{
				{Direction: "debit", AmountSats: math.MaxInt64},
				{Direction: "debit", AmountSats: 1},
				{Direction: "credit", AmountSats: 1},
			},
			wantErr: ErrInvalidLedgerEntry,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateEntries(test.entries)
			if err == nil {
				t.Fatal("validateEntries() succeeded, want an error")
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("validateEntries() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
