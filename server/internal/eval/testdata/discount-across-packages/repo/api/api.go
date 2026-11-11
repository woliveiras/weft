// Package api encodes orders for the HTTP API.
package api

import (
	"encoding/json"

	"example.com/shop/order"
)

type wireLine struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
}

type wireOrder struct {
	ID         string     `json:"id"`
	Lines      []wireLine `json:"lines"`
	TotalCents int        `json:"total_cents"`
}

// Encode returns the JSON of o, including the total it computes.
func Encode(o order.Order) ([]byte, error) {
	w := wireOrder{ID: o.ID, TotalCents: o.Total()}
	for _, l := range o.Lines {
		w.Lines = append(w.Lines, wireLine{SKU: l.SKU, Quantity: l.Quantity, UnitCents: l.UnitCents})
	}
	return json.Marshal(w)
}

// Decode reads an order from JSON and validates it. total_cents is ignored:
// the server always computes the total itself.
func Decode(b []byte) (order.Order, error) {
	var w wireOrder
	if err := json.Unmarshal(b, &w); err != nil {
		return order.Order{}, err
	}
	o := order.Order{ID: w.ID}
	for _, l := range w.Lines {
		o.Lines = append(o.Lines, order.Line{SKU: l.SKU, Quantity: l.Quantity, UnitCents: l.UnitCents})
	}
	if err := o.Validate(); err != nil {
		return order.Order{}, err
	}
	return o, nil
}
