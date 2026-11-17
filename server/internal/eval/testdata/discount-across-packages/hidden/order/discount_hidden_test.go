package order

import "testing"

func TestHiddenDiscountTotal(t *testing.T) {
	lines := []Line{{SKU: "pen", Quantity: 2, UnitCents: 999}}
	for pct, want := range map[int]int{0: 1998, 15: 1699, 50: 999, 100: 0} {
		o := Order{ID: "A1", Lines: lines, DiscountPercent: pct}
		if o.Subtotal() != 1998 || o.Total() != want {
			t.Errorf("%d%%: subtotal %d total %d, want 1998 and %d", pct, o.Subtotal(), o.Total(), want)
		}
	}
}

func TestHiddenDiscountValidate(t *testing.T) {
	for pct, ok := range map[int]bool{-1: false, 0: true, 100: true, 101: false} {
		err := Order{ID: "A1", DiscountPercent: pct}.Validate()
		if (err == nil) != ok {
			t.Errorf("Validate with %d%%: %v", pct, err)
		}
	}
}
