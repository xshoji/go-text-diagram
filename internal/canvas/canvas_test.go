package canvas

import (
	"strings"
	"testing"

	"github.com/xshoji/go-text-diagram/internal/route"
)

func TestLineComposition(t *testing.T) {
	t.Parallel()

	c := New(3, 3)
	c.Connect(route.Point{X: 0, Y: 1}, route.Point{X: 2, Y: 1}, "horizontal")
	c.Connect(route.Point{X: 1, Y: 0}, route.Point{X: 1, Y: 2}, "vertical")
	if got, want := c.String(Unicode), " │\n─┼─\n │\n"; got != want {
		t.Fatalf("unicode canvas:\n%q\nwant:\n%q", got, want)
	}
	if got, want := c.String(ASCII), " |\n-+-\n |\n"; got != want {
		t.Fatalf("ASCII canvas:\n%q\nwant:\n%q", got, want)
	}
}

func TestWideAndCombiningText(t *testing.T) {
	t.Parallel()

	c := New(8, 1)
	c.WriteText(route.Point{}, "界e\u0301👨‍👩‍👧‍👦")
	if got, want := c.String(Unicode), "界é👨‍👩‍👧‍👦\n"; got != want {
		t.Fatalf("canvas = %q, want %q", got, want)
	}
}

func TestStyledLines(t *testing.T) {
	t.Parallel()
	c := New(5, 5)
	c.ConnectStyled(route.Point{X: 0, Y: 0}, route.Point{X: 4, Y: 0}, "double", Double)
	c.ConnectStyled(route.Point{X: 0, Y: 2}, route.Point{X: 4, Y: 2}, "dotted", Dotted)
	c.ConnectStyled(route.Point{X: 0, Y: 4}, route.Point{X: 4, Y: 4}, "bold", Bold)
	got := c.String(Unicode)
	if !strings.Contains(got, "═════") || !strings.Contains(got, "┄┄┄┄┄") || !strings.Contains(got, "━━━━━") {
		t.Fatalf("styled canvas:\n%s", got)
	}
}
