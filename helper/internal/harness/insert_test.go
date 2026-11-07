package harness

import (
	"reflect"
	"testing"
)

func TestInsertionsPerPathPastesEachPathThenText(t *testing.T) {
	got := Insertions("fix the header", []string{"/u/a.png", "/u/my shot.jpg"}, InsertPerPath)
	want := []Insertion{
		{Text: "/u/a.png "},
		{Text: `/u/my\ shot.jpg `},
		{Text: "fix the header", Submit: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestInsertionsPerPathWithoutTextSubmitsLastPath(t *testing.T) {
	got := Insertions("  ", []string{"/u/a.png", "/u/b.png"}, InsertPerPath)
	want := []Insertion{{Text: "/u/a.png "}, {Text: "/u/b.png ", Submit: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
}

func TestInsertionsTogetherIsOnePaste(t *testing.T) {
	got := Insertions("look", []string{"/u/a.png", "/u/b.png"}, InsertTogether)
	want := []Insertion{{Text: "/u/a.png /u/b.png look", Submit: true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}
	// An unknown style is together too.
	if got := Insertions("look", []string{"/u/a.png"}, ""); len(got) != 1 || got[0].Text != "/u/a.png look" {
		t.Errorf("default: %#v", got)
	}
}

func TestInsertionsWithoutImagesIsTheText(t *testing.T) {
	got := Insertions("hi", nil, InsertPerPath)
	if !reflect.DeepEqual(got, []Insertion{{Text: "hi", Submit: true}}) {
		t.Errorf("got %#v", got)
	}
}

func TestValidateImagePaths(t *testing.T) {
	if err := ValidateImagePaths([]string{"/Users/a/.togo/uploads/s_x/1-image.png"}); err != nil {
		t.Errorf("good path refused: %v", err)
	}
	bad := [][]string{
		{"rel/a.png"}, {"/a/../b.png"}, {"/a/b.txt"}, {"/a/b\n.png"}, {""},
		make([]string, MaxSendImages+1),
	}
	for _, b := range bad {
		if ValidateImagePaths(b) == nil {
			t.Errorf("%q accepted", b)
		}
	}
}
