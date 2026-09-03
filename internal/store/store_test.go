package store

import "testing"

func TestFilterSQLUsesPlaceholdersAndRejectsUnsafeIdentifiers(t *testing.T) {
	query, args, err := filterSQL("sqlite", []Condition{{Column: "active", Op: "=", Value: true}})
	if err != nil {
		t.Fatal(err)
	}
	if query != ` WHERE "active" = ?` || len(args) != 1 || args[0] != true {
		t.Fatalf("unexpected query: %q %#v", query, args)
	}
	if _, _, err := filterSQL("sqlite", []Condition{{Column: "active; DROP TABLE users", Op: "=", Value: true}}); err == nil {
		t.Fatal("unsafe identifier was accepted")
	}
}
