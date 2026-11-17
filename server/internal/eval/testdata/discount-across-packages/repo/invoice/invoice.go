// Package invoice renders orders as plain-text invoices.
package invoice

import (
	"fmt"
	"strings"

	"example.com/shop/order"
)

// Render returns the invoice text for o.
func Render(o order.Order) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Invoice %s\n", o.ID)
	for _, l := range o.Lines {
		fmt.Fprintf(&b, "%d x %s @ %s\n", l.Quantity, l.SKU, money(l.UnitCents))
	}
	fmt.Fprintf(&b, "Subtotal: %s\n", money(o.Subtotal()))
	fmt.Fprintf(&b, "Total: %s\n", money(o.Total()))
	return b.String()
}

func money(cents int) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
