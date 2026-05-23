package render

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "keeps normal filename",
			input: "photo.jpg",
			want:  "photo.jpg",
		},
		{
			name:  "removes path separators",
			input: "../nested/path/photo.jpg",
			want:  "..nestedpathphoto.jpg",
		},
		{
			name:  "removes control characters",
			input: "bad\x00name\x1f.jpg",
			want:  "badname.jpg",
		},
		{
			name:  "removes windows reserved name",
			input: "con",
			want:  "",
		},
		{
			name:  "removes trailing windows characters",
			input: "photo. ",
			want:  "photo",
		},
		{
			name:  "truncates long names",
			input: strings.Repeat("a", 300),
			want:  strings.Repeat("a", 254),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeFilename(tt.input); got != tt.want {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
