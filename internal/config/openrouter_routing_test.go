package config

import (
	"os"
	"reflect"
	"testing"
)

func TestOpenRouterSort(t *testing.T) {
	tests := []struct {
		name string
		env  *string
		want string
	}{
		{name: "unset uses default", env: nil, want: DefaultOpenRouterSort},
		{name: "none disables", env: strPtr("none"), want: ""},
		{name: "off disables", env: strPtr("off"), want: ""},
		{name: "empty disables", env: strPtr(""), want: ""},
		{name: "custom", env: strPtr("latency"), want: "latency"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvOpenRouterSort, "placeholder")
			if tt.env == nil {
				if err := os.Unsetenv(EnvOpenRouterSort); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(EnvOpenRouterSort, *tt.env)
			}
			if got := OpenRouterSort(); got != tt.want {
				t.Fatalf("OpenRouterSort() = %q want %q", got, tt.want)
			}
		})
	}
}

func TestOpenRouterIgnore(t *testing.T) {
	tests := []struct {
		name string
		env  *string // nil = unset
		want []string
	}{
		{name: "unset uses defaults", env: nil, want: DefaultOpenRouterIgnore},
		{name: "none disables", env: strPtr("none"), want: nil},
		{name: "off disables", env: strPtr("off"), want: nil},
		{name: "empty disables", env: strPtr(""), want: nil},
		{name: "custom list", env: strPtr("Wafer, morph"), want: []string{"wafer", "morph"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvOpenRouterIgnore, "placeholder")
			if tt.env == nil {
				if err := os.Unsetenv(EnvOpenRouterIgnore); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(EnvOpenRouterIgnore, *tt.env)
			}
			got := OpenRouterIgnore()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("OpenRouterIgnore() = %v want %v", got, tt.want)
			}
		})
	}
}

func TestOpenRouterMaxLatencyP90(t *testing.T) {
	tests := []struct {
		name string
		env  *string
		want float64
	}{
		{name: "unset default", env: nil, want: DefaultOpenRouterMaxLatencyP90},
		{name: "off", env: strPtr("off"), want: 0},
		{name: "zero", env: strPtr("0"), want: 0},
		{name: "custom", env: strPtr("3.5"), want: 3.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvOpenRouterMaxLatencyP90, "placeholder")
			if tt.env == nil {
				if err := os.Unsetenv(EnvOpenRouterMaxLatencyP90); err != nil {
					t.Fatal(err)
				}
			} else {
				t.Setenv(EnvOpenRouterMaxLatencyP90, *tt.env)
			}
			if got := OpenRouterMaxLatencyP90(); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
