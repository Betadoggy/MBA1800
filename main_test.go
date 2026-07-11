package main

import "testing"

func TestPasswordHashing(t *testing.T) {
	password := "secret123"
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword failed: %v", err)
	}

	if !checkPasswordHash(password, hash) {
		t.Fatal("expected password to match hash")
	}

	if checkPasswordHash("wrong-password", hash) {
		t.Fatal("expected wrong password to fail")
	}
}

func TestFilterProblemsByIDs(t *testing.T) {
	problems := []Problem{{ID: 1, Category: "A"}, {ID: 2, Category: "B"}, {ID: 3, Category: "A"}}
	filtered := filterProblemsByIDs(problems, []int{2, 3})

	if len(filtered) != 2 {
		t.Fatalf("expected 2 problems, got %d", len(filtered))
	}

	if filtered[0].ID != 2 || filtered[1].ID != 3 {
		t.Fatalf("unexpected filtered order: %+v", filtered)
	}
}

func TestApplyChartWidths(t *testing.T) {
	groups := []WeaknessGroup{{Name: "A", WrongCount: 4}, {Name: "B", WrongCount: 2}, {Name: "C", WrongCount: 0}}
	applyChartWidths(groups)

	if groups[0].ChartWidth != 100 {
		t.Fatalf("expected first bar to be 100%% wide, got %d", groups[0].ChartWidth)
	}
	if groups[1].ChartWidth != 50 {
		t.Fatalf("expected second bar to be 50%% wide, got %d", groups[1].ChartWidth)
	}
	if groups[2].ChartWidth != 0 {
		t.Fatalf("expected zero-wrong group to have 0 width, got %d", groups[2].ChartWidth)
	}
}

func TestBuildWeaknessGroups(t *testing.T) {
	problems := []Problem{{ID: 1, Category: "경영일반"}, {ID: 2, Category: "마케팅 개요"}, {ID: 3, Category: "조직이론"}}
	progressEntries := []Progress{{ProblemID: 1, WrongCount: 2}, {ProblemID: 2, WrongCount: 1}, {ProblemID: 3, WrongCount: 3}}

	groups := buildWeaknessGroups(problems, progressEntries)
	if len(groups) != len(categoryGroups) {
		t.Fatalf("expected %d groups, got %d", len(categoryGroups), len(groups))
	}

	if groups[0].Name != categoryGroups[0].Name {
		t.Fatalf("expected first group %q, got %q", categoryGroups[0].Name, groups[0].Name)
	}

	if len(groups[0].Subcategories) == 0 {
		t.Fatal("expected subcategories to be present")
	}

	if groups[0].Subcategories[0].Name != "경영일반" {
		t.Fatalf("expected first subcategory to be 경영일반, got %q", groups[0].Subcategories[0].Name)
	}
	if groups[0].Subcategories[0].WrongCount != 2 {
		t.Fatalf("expected 경영일반 wrong count 2, got %d", groups[0].Subcategories[0].WrongCount)
	}

	if groups[1].Subcategories[0].Name != "마케팅 개요" {
		t.Fatalf("expected second group first subcategory to be 마케팅 개요, got %q", groups[1].Subcategories[0].Name)
	}
}
