// Package calc does small arithmetic for invoices.
package calc

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a - b
}

// Total adds every amount.
func Total(amounts []int) int {
	t := 0
	for _, a := range amounts {
		t = Add(t, a)
	}
	return t
}
