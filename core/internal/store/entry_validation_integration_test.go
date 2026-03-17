package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"mdm/core/internal/infra"
	"mdm/core/internal/migrations"
)

func TestValidateDataIntegration_ReferenceRequiredUnknown(t *testing.T) {
	db := openIntegrationDB(t)
	resetAndMigrate(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dictionaries := NewDictionaryRepository(db)
	attributes := NewAttributeRepository(db)
	schemas := NewDictionarySchemaRepository(db)
	entries := NewEntryRepository(db)

	categoryDictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "categories",
		Name: "Categories",
	})
	if err != nil {
		t.Fatalf("create categories dictionary: %v", err)
	}

	productDictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "products",
		Name: "Products",
	})
	if err != nil {
		t.Fatalf("create products dictionary: %v", err)
	}

	skuAttribute, err := attributes.Create(ctx, CreateAttributeInput{
		Code:     "sku",
		Name:     "SKU",
		DataType: "string",
	})
	if err != nil {
		t.Fatalf("create sku attribute: %v", err)
	}

	categoryAttribute, err := attributes.Create(ctx, CreateAttributeInput{
		Code:            "category_id",
		Name:            "Category",
		DataType:        "reference",
		RefDictionaryID: &categoryDictionary.ID,
	})
	if err != nil {
		t.Fatalf("create reference attribute: %v", err)
	}

	_, err = schemas.ReplaceByDictionaryID(ctx, productDictionary.ID, []ReplaceDictionarySchemaAttributeInput{
		{
			AttributeID: skuAttribute.ID,
			Required:    true,
			Position:    10,
		},
		{
			AttributeID: categoryAttribute.ID,
			Required:    true,
			Position:    20,
		},
	})
	if err != nil {
		t.Fatalf("replace dictionary schema: %v", err)
	}

	err = entries.ValidateData(ctx, productDictionary.ID, map[string]any{
		"sku": "SKU-1",
	}, nil)
	validationErr := requireEntryValidationError(t, err)
	assertContainsCodes(t, validationErr.Issues, "required")
	assertIssueFieldContains(t, validationErr.Issues, "required", "category_id")

	err = entries.ValidateData(ctx, productDictionary.ID, map[string]any{
		"sku":         "SKU-1",
		"category_id": "11111111-1111-1111-1111-111111111111",
		"ghost":       "value",
	}, nil)
	validationErr = requireEntryValidationError(t, err)
	assertContainsCodes(t, validationErr.Issues, "unknown_attribute", "reference_not_found")

	categoryEntry, err := entries.Create(ctx, CreateEntryInput{
		DictionaryID: categoryDictionary.ID,
		Data: map[string]any{
			"title": "Food",
		},
	})
	if err != nil {
		t.Fatalf("create category entry: %v", err)
	}

	err = entries.ValidateData(ctx, productDictionary.ID, map[string]any{
		"sku":         "SKU-1",
		"category_id": categoryEntry.ID,
	}, nil)
	if err != nil {
		t.Fatalf("validate data with existing reference: %v", err)
	}
}

func TestValidateDataIntegration_UniqueCreateAndUpdate(t *testing.T) {
	db := openIntegrationDB(t)
	resetAndMigrate(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	dictionaries := NewDictionaryRepository(db)
	attributes := NewAttributeRepository(db)
	schemas := NewDictionarySchemaRepository(db)
	entries := NewEntryRepository(db)

	dictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "products_unique",
		Name: "Products Unique",
	})
	if err != nil {
		t.Fatalf("create dictionary: %v", err)
	}

	skuAttribute, err := attributes.Create(ctx, CreateAttributeInput{
		Code:     "sku_unique",
		Name:     "SKU Unique",
		DataType: "string",
	})
	if err != nil {
		t.Fatalf("create attribute: %v", err)
	}

	_, err = schemas.ReplaceByDictionaryID(ctx, dictionary.ID, []ReplaceDictionarySchemaAttributeInput{
		{
			AttributeID: skuAttribute.ID,
			Required:    true,
			IsUnique:    true,
			Position:    10,
		},
	})
	if err != nil {
		t.Fatalf("replace schema: %v", err)
	}

	entryOne, err := entries.Create(ctx, CreateEntryInput{
		DictionaryID: dictionary.ID,
		Data: map[string]any{
			"sku_unique": "SKU-1",
		},
	})
	if err != nil {
		t.Fatalf("create first entry: %v", err)
	}

	err = entries.ValidateData(ctx, dictionary.ID, map[string]any{
		"sku_unique": "SKU-1",
	}, nil)
	validationErr := requireEntryValidationError(t, err)
	assertContainsCodes(t, validationErr.Issues, "not_unique")

	err = entries.ValidateData(ctx, dictionary.ID, map[string]any{
		"sku_unique": "SKU-1",
	}, &entryOne.ID)
	if err != nil {
		t.Fatalf("validate update with same value for same entry: %v", err)
	}

	_, err = entries.Create(ctx, CreateEntryInput{
		DictionaryID: dictionary.ID,
		Data: map[string]any{
			"sku_unique": "SKU-2",
		},
	})
	if err != nil {
		t.Fatalf("create second entry: %v", err)
	}

	err = entries.ValidateData(ctx, dictionary.ID, map[string]any{
		"sku_unique": "SKU-2",
	}, &entryOne.ID)
	validationErr = requireEntryValidationError(t, err)
	assertContainsCodes(t, validationErr.Issues, "not_unique")
}

func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("integration test skipped: TEST_DATABASE_DSN is not set")
	}

	if err := validateSafeTestDSN(dsn); err != nil {
		t.Fatalf("unsafe TEST_DATABASE_DSN: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := infra.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func validateSafeTestDSN(dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parse DSN: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errors.New("DSN must use postgres:// or postgresql://")
	}

	host := strings.ToLower(parsed.Hostname())
	allowedHosts := []string{"localhost", "127.0.0.1", "postgres-test"}
	if !slices.Contains(allowedHosts, host) {
		return fmt.Errorf("host %q is not in allowed test hosts: %s", host, strings.Join(allowedHosts, ", "))
	}

	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if databaseName == "" {
		return errors.New("database name is empty")
	}
	if !strings.HasSuffix(strings.ToLower(databaseName), "_test") {
		return fmt.Errorf("database name %q must end with _test", databaseName)
	}
	if strings.Contains(strings.ToLower(databaseName), "prod") {
		return fmt.Errorf("database name %q looks like production", databaseName)
	}

	return nil
}

func resetAndMigrate(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const resetSQL = `
		DROP SCHEMA IF EXISTS public CASCADE;
		CREATE SCHEMA public;
	`
	if _, err := db.ExecContext(ctx, resetSQL); err != nil {
		t.Fatalf("reset database schema: %v", err)
	}

	if err := migrations.Run(ctx, db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
}

func requireEntryValidationError(t *testing.T, err error) EntryValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	validationErr, ok := IsEntryValidationError(err)
	if !ok {
		t.Fatalf("expected EntryValidationError, got %T: %v", err, err)
	}
	return validationErr
}

func assertIssueFieldContains(t *testing.T, issues []EntryValidationIssue, code, field string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.Field == field {
			return
		}
	}
	t.Fatalf("expected issue code=%q field=%q in %+v", code, field, issues)
}
