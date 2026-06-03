package suites

import (
	"testing"
)

func TestGetReturnsClonedSuite(t *testing.T) {
	service := &Service{suites: map[string]Definition{}}
	_, err := service.Register(RegisterRequest{
		ID:        "clone-test",
		Title:     "Clone Test",
		SuiteStar: `api = service.run(name="api")`,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	suite, err := service.Get("clone-test")
	if err != nil {
		t.Fatalf("get suite: %v", err)
	}
	suite.Title = "mutated"

	reloaded, err := service.Get("clone-test")
	if err != nil {
		t.Fatalf("get suite again: %v", err)
	}
	if reloaded.Title == "mutated" {
		t.Fatal("expected suite to be returned as a clone, not a shared reference")
	}
}

func TestListReturnsSortedSuites(t *testing.T) {
	service := &Service{suites: map[string]Definition{}}
	for _, id := range []string{"zebra-suite", "alpha-suite", "mango-suite"} {
		if _, err := service.Register(RegisterRequest{ID: id, SuiteStar: `api = service.run(name="api")`}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}

	items := service.List()
	if len(items) != 3 {
		t.Fatalf("expected 3 suites, got %d", len(items))
	}
	if items[0].Title != "Alpha Suite" {
		t.Fatalf("expected sorted suites, got %q first", items[0].Title)
	}
	if items[2].Title != "Zebra Suite" {
		t.Fatalf("expected zebra suite last, got %q", items[2].Title)
	}
}
