package app

import (
	"testing"

	"pilot/internal/protocol"
)

func TestMoney(t *testing.T) {
	cases := []struct {
		spend *protocol.TaskSpend
		want  string
	}{
		{nil, ""},
		{&protocol.TaskSpend{Cost: 0}, ""},
		{&protocol.TaskSpend{Cost: 0.0312}, "$0.031"},
		{&protocol.TaskSpend{Cost: 1.234}, "$1.23"},
	}
	for _, c := range cases {
		if got := money(c.spend); got != c.want {
			t.Fatalf("money(%v) = %q, want %q", c.spend, got, c.want)
		}
	}
}
