package ledger

import (
	"errors"
	"math"
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
