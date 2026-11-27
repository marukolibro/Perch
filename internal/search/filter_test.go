package search

import (
	"slices"
	"testing"
)

func TestPathFilterIncludeAndExclude(t *testing.T) {
	paths := []string{
		"README.md",
		"docs/product.md",
		"internal/app/app.go",
		"internal/app/app_test.go",
		"internal/app/view.go",
		"internal/review/review.go",
		"internal/review/review_test.go",
		"vendor/pkg/dep.go",
	}

	compiled, err := PathFilter{
		Include: "internal/app, *.md",
		Exclude: "*_test.go, docs/",
	}.Compile()
	if err != nil {
		t.Fatalf("compile filter: %v", err)
	}
	got := FilterPaths(paths, compiled)
	want := []string{"README.md", "internal/app/app.go", "internal/app/view.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("filtered = %v, want %v", got, want)
	}

	// Recursive glob with **.
	globFilter, err := PathFilter{Include: "internal/**/*.go", Exclude: "*_test.go"}.Compile()
	if err != nil {
		t.Fatalf("compile glob: %v", err)
	}
	got = FilterPaths(paths, globFilter)
	want = []string{"internal/app/app.go", "internal/app/view.go", "internal/review/review.go"}
	if !slices.Equal(got, want) {
		t.Fatalf("glob filtered = %v, want %v", got, want)
	}

	// Single folder name excludes everything under that folder.
	folderFilter, err := PathFilter{Exclude: "vendor"}.Compile()
	if err != nil {
		t.Fatalf("compile folder filter: %v", err)
	}
	if folderFilter.Match("vendor/pkg/dep.go") {
		t.Error("vendor filter matched vendor/pkg/dep.go")
	}
	if !folderFilter.Match("internal/app/app.go") {
		t.Error("vendor filter rejected internal/app/app.go")
	}

	// Invalid glob returns an error.
	if _, err := (PathFilter{Include: "[unclosed"}).Compile(); err == nil {
		t.Error("expected invalid glob to fail")
	}
}
