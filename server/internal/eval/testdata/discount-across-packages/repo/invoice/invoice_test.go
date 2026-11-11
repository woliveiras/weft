package invoice

import (
	"testing"

	"example.com/shop/order"
)

func TestRender(t *testing.T) {
	got := Render(order.Order{ID: "A1", Lines: []order.Line{{SKU: "pen", Quantity: 2, UnitCents: 150}}})
	want := "Invoice A1\n2 x pen @ 1.50\nSubtotal: 3.00\nTotal: 3.00\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
