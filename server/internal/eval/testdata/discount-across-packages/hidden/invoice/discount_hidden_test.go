package invoice

import (
	"testing"

	"example.com/shop/order"
)

func TestHiddenRenderDiscount(t *testing.T) {
	o := order.Order{ID: "A1", Lines: []order.Line{{SKU: "pen", Quantity: 2, UnitCents: 999}}, DiscountPercent: 15}
	want := "Invoice A1\n2 x pen @ 9.99\nSubtotal: 19.98\nDiscount: 15%\nTotal: 16.99\n"
	if got := Render(o); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	o.DiscountPercent = 0
	if got, want := Render(o), "Invoice A1\n2 x pen @ 9.99\nSubtotal: 19.98\nTotal: 19.98\n"; got != want {
		t.Fatalf("without a discount: got %q, want %q", got, want)
	}
}
