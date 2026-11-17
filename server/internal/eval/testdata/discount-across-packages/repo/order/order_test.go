package order

import "testing"

func TestTotal(t *testing.T) {
	o := Order{ID: "A1", Lines: []Line{{SKU: "pen", Quantity: 2, UnitCents: 150}, {SKU: "ink", Quantity: 1, UnitCents: 300}}}
	if o.Subtotal() != 600 || o.Total() != 600 {
		t.Fatal(o.Subtotal(), o.Total())
	}
}

func TestValidate(t *testing.T) {
	if err := (Order{ID: "A1", Lines: []Line{{SKU: "pen", Quantity: 0}}}).Validate(); err == nil {
		t.Fatal("zero quantity must fail")
	}
	if err := (Order{}).Validate(); err == nil {
		t.Fatal("missing id must fail")
	}
}
