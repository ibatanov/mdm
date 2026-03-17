package store

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

type referenceFixture struct {
	countryDictionaryID      string
	manufacturerDictionaryID string
	productDictionaryID      string
	countryID                string
	manufacturerID           string
	productID                string
}

func TestResolveEntryIntegration_NestedReferences(t *testing.T) {
	db := openIntegrationDB(t)
	resetAndMigrate(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	entries := NewEntryRepository(db)
	fixture := prepareNestedReferenceFixture(t, ctx, db)

	product, err := entries.GetByID(ctx, fixture.productDictionaryID, fixture.productID)
	if err != nil {
		t.Fatalf("get product entry: %v", err)
	}

	resolved, err := entries.ResolveEntry(ctx, product)
	if err != nil {
		t.Fatalf("resolve product entry: %v", err)
	}

	manufacturerRaw, ok := resolved.Data["manufacturer"]
	if !ok {
		t.Fatalf("resolved entry does not contain manufacturer field: %+v", resolved.Data)
	}
	manufacturer, ok := manufacturerRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected resolved manufacturer as object, got %T (%v)", manufacturerRaw, manufacturerRaw)
	}
	if got := manufacturer["name"]; got != "BMW" {
		t.Fatalf("expected manufacturer.name = BMW, got %v", got)
	}

	countryRaw, ok := manufacturer["country"]
	if !ok {
		t.Fatalf("resolved manufacturer does not contain country field: %+v", manufacturer)
	}
	country, ok := countryRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected resolved country as object, got %T (%v)", countryRaw, countryRaw)
	}
	if got := country["name"]; got != "Germany" {
		t.Fatalf("expected country.name = Germany, got %v", got)
	}
}

func TestSearchByDictionaryIDIntegration_ReferenceFilters(t *testing.T) {
	db := openIntegrationDB(t)
	resetAndMigrate(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	entries := NewEntryRepository(db)
	fixture := prepareNestedReferenceFixture(t, ctx, db)

	byManufacturer, err := entries.SearchByDictionaryID(ctx, SearchEntriesInput{
		DictionaryID: fixture.productDictionaryID,
		Filters: []EntrySearchFilter{
			{
				Attribute: "manufacturer",
				Op:        "eq",
				Value:     "BMW",
			},
		},
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("search products by manufacturer name: %v", err)
	}
	if byManufacturer.Total != 1 {
		t.Fatalf("expected total=1 for manufacturer search, got %d", byManufacturer.Total)
	}
	if len(byManufacturer.Items) != 1 {
		t.Fatalf("expected 1 item for manufacturer search, got %d", len(byManufacturer.Items))
	}
	if byManufacturer.Items[0].ID != fixture.productID {
		t.Fatalf("expected product id %s, got %s", fixture.productID, byManufacturer.Items[0].ID)
	}

	byNestedCountry, err := entries.SearchByDictionaryID(ctx, SearchEntriesInput{
		DictionaryID: fixture.productDictionaryID,
		Filters: []EntrySearchFilter{
			{
				Attribute: "manufacturer",
				Op:        "contains",
				Value:     "germ",
			},
		},
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("search products by nested country token: %v", err)
	}
	if byNestedCountry.Total != 1 {
		t.Fatalf("expected total=1 for nested country search, got %d", byNestedCountry.Total)
	}
	if len(byNestedCountry.Items) != 1 {
		t.Fatalf("expected 1 item for nested country search, got %d", len(byNestedCountry.Items))
	}
	if byNestedCountry.Items[0].ID != fixture.productID {
		t.Fatalf("expected product id %s for nested country search, got %s", fixture.productID, byNestedCountry.Items[0].ID)
	}

	notFound, err := entries.SearchByDictionaryID(ctx, SearchEntriesInput{
		DictionaryID: fixture.productDictionaryID,
		Filters: []EntrySearchFilter{
			{
				Attribute: "manufacturer",
				Op:        "eq",
				Value:     "Audi",
			},
		},
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("search products by unknown manufacturer: %v", err)
	}
	if notFound.Total != 0 {
		t.Fatalf("expected total=0 for unknown manufacturer search, got %d", notFound.Total)
	}
	if len(notFound.Items) != 0 {
		t.Fatalf("expected 0 items for unknown manufacturer search, got %d", len(notFound.Items))
	}
}

func prepareNestedReferenceFixture(t *testing.T, ctx context.Context, db *sql.DB) referenceFixture {
	t.Helper()

	dictionaries := NewDictionaryRepository(db)
	attributes := NewAttributeRepository(db)
	schemas := NewDictionarySchemaRepository(db)
	entries := NewEntryRepository(db)

	countryDictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "countries",
		Name: "Countries",
	})
	if err != nil {
		t.Fatalf("create countries dictionary: %v", err)
	}

	manufacturerDictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "manufacturers",
		Name: "Manufacturers",
	})
	if err != nil {
		t.Fatalf("create manufacturers dictionary: %v", err)
	}

	productDictionary, err := dictionaries.Create(ctx, CreateDictionaryInput{
		Code: "products",
		Name: "Products",
	})
	if err != nil {
		t.Fatalf("create products dictionary: %v", err)
	}

	nameAttr, err := attributes.Create(ctx, CreateAttributeInput{
		Code:     "name",
		Name:     "Name",
		DataType: "string",
	})
	if err != nil {
		t.Fatalf("create name attribute: %v", err)
	}

	countryAttr, err := attributes.Create(ctx, CreateAttributeInput{
		Code:            "country",
		Name:            "Country",
		DataType:        "reference",
		RefDictionaryID: &countryDictionary.ID,
	})
	if err != nil {
		t.Fatalf("create country reference attribute: %v", err)
	}

	manufacturerAttr, err := attributes.Create(ctx, CreateAttributeInput{
		Code:            "manufacturer",
		Name:            "Manufacturer",
		DataType:        "reference",
		RefDictionaryID: &manufacturerDictionary.ID,
	})
	if err != nil {
		t.Fatalf("create manufacturer reference attribute: %v", err)
	}

	_, err = schemas.ReplaceByDictionaryID(ctx, countryDictionary.ID, []ReplaceDictionarySchemaAttributeInput{
		{
			AttributeID: nameAttr.ID,
			Required:    true,
			Position:    10,
		},
	})
	if err != nil {
		t.Fatalf("replace countries schema: %v", err)
	}

	_, err = schemas.ReplaceByDictionaryID(ctx, manufacturerDictionary.ID, []ReplaceDictionarySchemaAttributeInput{
		{
			AttributeID: nameAttr.ID,
			Required:    true,
			Position:    10,
		},
		{
			AttributeID: countryAttr.ID,
			Required:    true,
			Position:    20,
		},
	})
	if err != nil {
		t.Fatalf("replace manufacturers schema: %v", err)
	}

	_, err = schemas.ReplaceByDictionaryID(ctx, productDictionary.ID, []ReplaceDictionarySchemaAttributeInput{
		{
			AttributeID: nameAttr.ID,
			Required:    true,
			Position:    10,
		},
		{
			AttributeID: manufacturerAttr.ID,
			Required:    true,
			Position:    20,
		},
	})
	if err != nil {
		t.Fatalf("replace products schema: %v", err)
	}

	countryEntry, err := entries.Create(ctx, CreateEntryInput{
		DictionaryID: countryDictionary.ID,
		Data: map[string]any{
			"name": "Germany",
		},
	})
	if err != nil {
		t.Fatalf("create country entry: %v", err)
	}

	manufacturerEntry, err := entries.Create(ctx, CreateEntryInput{
		DictionaryID: manufacturerDictionary.ID,
		Data: map[string]any{
			"name":    "BMW",
			"country": countryEntry.ID,
		},
	})
	if err != nil {
		t.Fatalf("create manufacturer entry: %v", err)
	}

	productEntry, err := entries.Create(ctx, CreateEntryInput{
		DictionaryID: productDictionary.ID,
		Data: map[string]any{
			"name":         "X5",
			"manufacturer": manufacturerEntry.ID,
		},
	})
	if err != nil {
		t.Fatalf("create product entry: %v", err)
	}

	return referenceFixture{
		countryDictionaryID:      countryDictionary.ID,
		manufacturerDictionaryID: manufacturerDictionary.ID,
		productDictionaryID:      productDictionary.ID,
		countryID:                countryEntry.ID,
		manufacturerID:           manufacturerEntry.ID,
		productID:                productEntry.ID,
	}
}
