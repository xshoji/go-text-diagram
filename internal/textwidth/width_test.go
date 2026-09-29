package textwidth

import "testing"

func TestString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "ASCII", text: "Client", want: 6},
		{name: "CJK", text: "利用者", want: 6},
		{name: "East Asian Ambiguous", text: "·", want: 1},
		{name: "combining", text: "e\u0301", want: 1},
		{name: "emoji", text: "🙂", want: 2},
		{name: "emoji ZWJ sequence", text: "👨‍👩‍👧‍👦", want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := String(test.text); got != test.want {
				t.Fatalf("String(%q) = %d, want %d", test.text, got, test.want)
			}
		})
	}
}

func TestClusters(t *testing.T) {
	t.Parallel()

	clusters := Clusters("Ae\u0301👨‍👩‍👧‍👦")
	if len(clusters) != 3 {
		t.Fatalf("clusters = %#v", clusters)
	}
	if clusters[1].Text != "é" || clusters[1].Width != 1 || clusters[2].Width != 2 {
		t.Fatalf("clusters = %#v", clusters)
	}
}

func TestLines(t *testing.T) {
	t.Parallel()

	lines, width := Lines("A\n日本語")
	if len(lines) != 2 || width != 6 {
		t.Fatalf("Lines returned %#v, %d", lines, width)
	}
}
