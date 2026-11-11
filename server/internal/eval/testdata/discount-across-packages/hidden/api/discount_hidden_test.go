package api

import (
	"strings"
	"testing"

	"example.com/shop/order"
)

func TestHiddenDiscountWire(t *testing.T) {
	in := order.Order{ID: "A1", Lines: []order.Line{{SKU: "pen", Quantity: 2, UnitCents: 999}}, DiscountPercent: 15}
	b, err := Encode(in)
	if err != nil || !strings.Contains(string(b), `"discount_percent":15`) || !strings.Contains(string(b), `"total_cents":1699`) {
		t.Fatalf("%s %v", b, err)
	}
	out, err := Decode(b)
	if err != nil || out.DiscountPercent != 15 || out.Total() != 1699 {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err := Decode([]byte(`{"id":"A2","lines":[{"sku":"pen","quantity":1,"unit_cents":100}],"discount_percent":120}`)); err == nil {
		t.Fatal("a discount above 100% must be rejected")
	}
}
