package game

import (
	"testing"
)

func TestExpToLevel(t *testing.T) {
	cases := []struct {
		exp      int64
		expected int
	}{
		{606868, 72}, // Conta 1 (kbelludoo)
		{4255, 17},   // Conta 2 (LrN)
		{194, 3},     // Conta 3 (kbelludoo2)
		{781, 8},     // Conta 4 (kbelludool)
		{26871, 33},  // Conta 5 (kbelludooom)
	}

	for _, c := range cases {
		lvl := ExpToLevel(c.exp)
		if lvl != c.expected {
			t.Errorf("ExpToLevel(%d) = %d; expected %d", c.exp, lvl, c.expected)
		}
	}
}
