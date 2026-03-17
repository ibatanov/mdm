package store

import (
	"slices"
	"testing"
)

func TestValidateSingleValueStringValidators(t *testing.T) {
	attribute := schemaValidationAttribute{
		Code:     "title",
		DataType: "string",
		Validators: map[string]any{
			"min_length": 3.0,
			"max_length": 5.0,
			"pattern":    "^[A-Z]+$",
		},
	}

	issues, _ := validateSingleValue(attribute, "title", "AB")
	assertContainsCodes(t, issues, "min_length")

	issues, _ = validateSingleValue(attribute, "title", "abcdef")
	assertContainsCodes(t, issues, "max_length", "pattern_mismatch")

	issues, _ = validateSingleValue(attribute, "title", "ABCD")
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %+v", issues)
	}
}

func TestValidateSingleValueNumberValidators(t *testing.T) {
	attribute := schemaValidationAttribute{
		Code:     "price",
		DataType: "number",
		Validators: map[string]any{
			"min": 10.0,
			"max": 20.0,
		},
	}

	issues, _ := validateSingleValue(attribute, "price", 9.0)
	assertContainsCodes(t, issues, "min")

	issues, _ = validateSingleValue(attribute, "price", 21.0)
	assertContainsCodes(t, issues, "max")

	issues, _ = validateSingleValue(attribute, "price", "10")
	assertContainsCodes(t, issues, "type_mismatch")
}

func TestValidateSingleValueDateValidators(t *testing.T) {
	attribute := schemaValidationAttribute{
		Code:     "start_date",
		DataType: "date",
		Validators: map[string]any{
			"min_date": "2024-01-01",
			"max_date": "2024-12-31",
		},
	}

	issues, _ := validateSingleValue(attribute, "start_date", "2023-12-31")
	assertContainsCodes(t, issues, "min_date")

	issues, _ = validateSingleValue(attribute, "start_date", "2025-01-01")
	assertContainsCodes(t, issues, "max_date")

	issues, _ = validateSingleValue(attribute, "start_date", "31-12-2024")
	assertContainsCodes(t, issues, "invalid_date")
}

func TestValidateSingleValueEnumAndReference(t *testing.T) {
	refDictionaryID := "11111111-1111-1111-1111-111111111111"

	enumAttribute := schemaValidationAttribute{
		Code:     "status",
		DataType: "enum",
		Validators: map[string]any{
			"allowed_values": []any{"draft", "published"},
		},
	}
	issues, _ := validateSingleValue(enumAttribute, "status", "archived")
	assertContainsCodes(t, issues, "enum_not_allowed")

	refAttribute := schemaValidationAttribute{
		Code:            "category_id",
		DataType:        "reference",
		RefDictionaryID: &refDictionaryID,
	}
	issues, ref := validateSingleValue(refAttribute, "category_id", "11111111-1111-1111-1111-111111111111")
	if len(issues) != 0 {
		t.Fatalf("expected no issues for valid reference, got %+v", issues)
	}
	if ref == nil || *ref != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected extracted reference id, got %v", ref)
	}

	issues, _ = validateSingleValue(refAttribute, "category_id", "invalid")
	assertContainsCodes(t, issues, "invalid_reference_uuid")
}

func TestValidateAttributeValueMultivalue(t *testing.T) {
	attribute := schemaValidationAttribute{
		Code:         "tags",
		DataType:     "string",
		IsMultivalue: true,
		Required:     true,
		Validators: map[string]any{
			"min_items": 1.0,
			"max_items": 2.0,
		},
	}

	issues, _ := validateAttributeValue(attribute, "tags", []any{})
	assertContainsCodes(t, issues, "min_items", "required")

	issues, _ = validateAttributeValue(attribute, "tags", []any{"ok", 1.0})
	assertContainsCodes(t, issues, "type_mismatch")
	if issues[0].Field != "tags[1]" {
		t.Fatalf("expected issue on tags[1], got %s", issues[0].Field)
	}

	issues, _ = validateAttributeValue(attribute, "tags", "not-array")
	assertContainsCodes(t, issues, "invalid_multivalue_type")
}

func TestUniqueCandidatesFromValue(t *testing.T) {
	attribute := schemaValidationAttribute{
		Code:         "tags",
		IsMultivalue: true,
	}

	candidates, issues := uniqueCandidatesFromValue(attribute, "tags", []any{"a", "a", "b"})
	assertContainsCodes(t, issues, "duplicate_value")
	if len(candidates) != 2 {
		t.Fatalf("expected 2 unique candidates, got %d", len(candidates))
	}
}

func assertContainsCodes(t *testing.T, issues []EntryValidationIssue, expected ...string) {
	t.Helper()
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	for _, code := range expected {
		if !slices.Contains(codes, code) {
			t.Fatalf("expected code %q in issues %+v", code, issues)
		}
	}
}
