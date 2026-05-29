package middleware

import (
	"testing"
)

func TestScanForPHI_MRN(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantPHI PHIType
		wantHit bool
	}{
		{"mrn prefix digits", "Patient MRN: 1234567", PHITypeMRN, true},
		{"mrn prefix no space", "MRN#9876543", PHITypeMRN, true},
		{"medical record long form", "Medical Record: 4567890", PHITypeMRN, true},
		{"clean text", "The patient was seen today", PHITypeMRN, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPHI(tt.text)
			hit := containsPHI(found, tt.wantPHI)
			if hit != tt.wantHit {
				t.Errorf("ScanForPHI(%q) for type %s: got hit=%v, want %v (found=%v)",
					tt.text, tt.wantPHI, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPHI_NPI(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"npi with prefix", "Provider NPI: 1234567890", true},
		{"npi colon no space", "NPI:2345678901", true},
		{"npi starts with 3 invalid", "NPI: 3234567890", false}, // must start with 1 or 2
		{"no npi", "Provider identifier: ABC123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPHI(tt.text)
			hit := containsPHI(found, PHITypeNPI)
			if hit != tt.wantHit {
				t.Errorf("ScanForPHI(%q) NPI: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPHI_ICD(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"diagnosis with code", "Diagnosis: E11.9", true},
		{"dx shorthand", "Dx: J18.9", true},
		{"icd prefix", "ICD: Z87.39", true},
		{"bare code no context", "E11.9", false}, // no keyword → not flagged
		{"clean", "Patient is recovering well", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPHI(tt.text)
			hit := containsPHI(found, PHITypeICD)
			if hit != tt.wantHit {
				t.Errorf("ScanForPHI(%q) ICD: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPHI_DEA(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"valid dea number", "DEA: AB1234567", true},
		{"valid dea registrant type F", "FX9876543", true},
		{"invalid registrant letter G", "GX1234567", false}, // G not in valid set
		{"clean", "No controlled substances prescribed", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPHI(tt.text)
			hit := containsPHI(found, PHITypeDEA)
			if hit != tt.wantHit {
				t.Errorf("ScanForPHI(%q) DEA: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPHI_ClinicalDate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantHit bool
	}{
		{"admission date", "Admission: 03/15/2023", true},
		{"discharge date dashes", "Discharge: 04-01-2023", true},
		{"admit date keyword", "Admit Date: 12/01/23", true},
		{"plain date no keyword", "03/15/2023", false},
		{"clean text", "Scheduled for follow-up", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := ScanForPHI(tt.text)
			hit := containsPHI(found, PHITypeClinicalDate)
			if hit != tt.wantHit {
				t.Errorf("ScanForPHI(%q) ClinicalDate: got %v, want %v (found=%v)",
					tt.text, hit, tt.wantHit, found)
			}
		})
	}
}

func TestScanForPHI_CleanText(t *testing.T) {
	clean := []string{
		"The weather is nice today.",
		"Please summarize this document.",
		"List the top 10 programming languages.",
		"What is the capital of France?",
	}
	for _, text := range clean {
		t.Run(text, func(t *testing.T) {
			found := ScanForPHI(text)
			if len(found) != 0 {
				t.Errorf("ScanForPHI(%q): expected empty, got %v", text, found)
			}
		})
	}
}

func TestScanForPHI_DeduplicatesTypes(t *testing.T) {
	// Both MRN rules match — should only produce one PHITypeMRN entry.
	text := "MRN: 1234567 Medical Record: 7654321"
	found := ScanForPHI(text)
	count := 0
	for _, p := range found {
		if p == PHITypeMRN {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 PHITypeMRN, got %d (found=%v)", count, found)
	}
}

// containsPHI is a test helper that checks if a PHIType is present in a slice.
func containsPHI(types []PHIType, target PHIType) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}
