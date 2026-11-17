package api

import (
	"strings"
	"testing"

	"example.com/shop/order"
)

func TestRoundTrip(t *testing.T) {
	in := order.Order{ID: "A1", Lines: []order.Line{{SKU: "pen", Quantity: 2, UnitCents: 150}}}
	b, err := Encode(in)
	if err != nil || !strings.Contains(string(b), `"total_cents":300`) {
		t.Fatal(string(b), err)
	}
	out, err := Decode(b)
	if err != nil || out.ID != "A1" || len(out.Lines) != 1 || out.Total() != 300 {
		t.Fatal(out, err)
	}
}

func TestDecodeValidates(t *testing.T) {
	if _, err := Decode([]byte(`{"id":"","lines":[]}`)); err == nil {
		t.Fatal("an order without id must be rejected")
	}
}
