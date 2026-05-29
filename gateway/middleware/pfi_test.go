package middleware

import (
	"testing"
)

func containsPFI(types []PFIType, want PFIType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func TestScanForPFI_IBAN(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"GB IBAN", "Transfer to GB29NWBK60161331926819", true},
		{"DE IBAN", "Account: DE89370400440532013000", true},
		{"FR IBAN", "IBAN FR7630006000011234567890189", true},
		{"clean text", "Please process the transfer", false},
		{"partial digits only", "Reference 12345678", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeIBAN)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) IBAN: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_SWIFT(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"wire keyword 8-char BIC", "wire DEUTDEDB settlement", true},
		{"BIC keyword 11-char", "BIC: BOFAUS3NXXX", true},
		{"SWIFT keyword", "SWIFT: CHASUS33XXX", true},
		{"no keyword bare BIC", "DEUTDEDB", false},
		{"short invalid", "ABCD", false},
		{"clean sentence", "Please send funds today", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeSWIFT)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) SWIFT: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_CUSIP(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"Apple CUSIP", "Security 037833100 settled", true},
		{"with letter", "CUSIP 38259P508", true},
		{"too short", "03783310", false},
		{"clean text", "Portfolio rebalancing complete", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeCUSIP)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) CUSIP: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_ISIN(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"Apple ISIN", "Hold US0378331005 long", true},
		{"Deutsche ISIN", "ISIN: DE0005140008", true},
		{"too short", "US037833100", false},
		{"no match", "Review the portfolio", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeISIN)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) ISIN: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_RoutingNumber(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"valid ABA", "Routing: 021000021", true},
		{"starts with 00", "ABA 001234567", true},
		{"starts with 4 invalid", "ABA 412345678", false},
		{"8 digits short", "ABA 01234567", false},
		{"clean", "Direct deposit set up", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeRoutingNum)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) RoutingNumber: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_AccountNumber(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"account keyword", "account #12345678901", true},
		{"acct keyword", "acct: 98765432101234", true},
		{"a/c keyword", "a/c 12345678901234567", true},
		{"no keyword", "12345678901", false},
		{"too short after keyword", "account #1234567", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeAccountNum)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) AccountNumber: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_EIN(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"EIN keyword", "EIN: 12-3456789", true},
		{"tax id keyword", "Tax ID: 98-7654321", true},
		{"no keyword bare", "12-3456789", false},
		{"clean", "File your taxes on time", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPFI(tt.text)
			hit := containsPFI(found, PFITypeTaxID)
			if hit != tt.wantHit {
				t.Errorf("ScanForPFI(%q) EIN: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPFI_CleanText(t *testing.T) {
	clean := []string{
		"Please summarize the quarterly earnings report.",
		"What is the current interest rate policy from the Fed?",
		"Analyze the risk-adjusted returns for this portfolio.",
		"Generate a memo about expense reimbursement procedures.",
	}
	for _, text := range clean {
		types := ScanForPFI(text)
		if len(types) != 0 {
			t.Errorf("ScanForPFI(%q): expected no PFI, got %v", text, types)
		}
	}
}

func TestScanForPFI_MultipleTypes(t *testing.T) {
	// A message with both IBAN and SWIFT should return both types.
	text := "Wire to GB29NWBK60161331926819 via BIC: DEUTDEDB"
	types := ScanForPFI(text)
	if !containsPFI(types, PFITypeIBAN) {
		t.Errorf("expected IBAN in %v", types)
	}
	if !containsPFI(types, PFITypeSWIFT) {
		t.Errorf("expected SWIFT in %v", types)
	}
}
