package main

import "testing"

func TestHealthStatus(t *testing.T) {
	tests := []struct {
		health   *health
		expected string
	}{
		{
			health: &health{
				drainFileExists: true,
				maintFileExists: false,
			},
			expected: "drain",
		},
		{
			health: &health{
				drainFileExists: false,
				maintFileExists: true,
			},
			expected: "maint",
		},
		{
			health: &health{
				drainFileExists: true,
				maintFileExists: true,
			},
			expected: "maint",
		},
		{
			health: &health{
				drainFileExists: false,
				maintFileExists: false,
			},
			expected: "ready",
		},
	}

	for _, test := range tests {
		output := test.health.Status()
		if output != test.expected {
			t.Fatalf("%v should have had the status '%v', but was '%v'", test, test.expected, output)
		}
	}
}
