// Package order models customer orders.
package order

import (
	"errors"
	"fmt"
)

// Line is one product in an order. Prices are in cents.
type Line struct {
	SKU       string
	Quantity  int
	UnitCents int
}

// Order is a customer order.
type Order struct {
	ID    string
	Lines []Line
}

// Subtotal is the sum of every line, in cents.
func (o Order) Subtotal() int {
	total := 0
	for _, l := range o.Lines {
		total += l.Quantity * l.UnitCents
	}
	return total
}

// Total is the amount the customer pays, in cents.
func (o Order) Total() int {
	return o.Subtotal()
}

// Validate reports the first problem with o.
func (o Order) Validate() error {
	if o.ID == "" {
		return errors.New("order: missing id")
	}
	for i, l := range o.Lines {
		if l.Quantity <= 0 {
			return fmt.Errorf("order %s: line %d: quantity must be positive", o.ID, i)
		}
	}
	return nil
}
