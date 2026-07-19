package syncstore

import "testing"

func TestNormalizeFileID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"files/t1/i1/photo.jpg", "t1/i1/photo.jpg", false},
		{"t1/i1/photo.jpg", "t1/i1/photo.jpg", false},
		{"../etc/passwd", "", true},
		{"files/../etc/passwd", "", true},
		{"/absolute", "", true},
		{"", "", true},
		{`t1\i1\x`, "t1/i1/x", false},
		{"files/", "", true},
		{"a/b/../../c", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeFileID(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidateSegmentID(t *testing.T) {
	if err := ValidateSegmentID("abc-123"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSegmentID(".."); err == nil {
		t.Fatal("expected error")
	}
	if err := ValidateSegmentID("a/b"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIsAllowedUserDataFile(t *testing.T) {
	if !IsAllowedUserDataFile("item-types.json") {
		t.Fatal("item-types.json should be allowed")
	}
	if IsAllowedUserDataFile("custom-item-presets.json") {
		t.Fatal("custom-item-presets.json should not be allowed")
	}
}

func TestResolveUnderRootTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := ResolveUnderRoot(root, "../outside"); err == nil {
		t.Fatal("expected traversal reject")
	}
	p, err := ResolveUnderRoot(root, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if p == "" {
		t.Fatal("empty path")
	}
}
