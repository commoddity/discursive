package start

import (
	"reflect"
	"testing"
)

func TestBackgroundChildArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "strips background",
			args: []string{"discursive", "start", "--background"},
			want: []string{"start", "--_bg"},
		},
		{
			name: "strips tunnel flag pair",
			args: []string{"discursive", "start", "--tunnel", "quick", "--background"},
			want: []string{"start", "--_bg"},
		},
		{
			name: "strips public-url flag pair",
			args: []string{"discursive", "start", "--public-url", "https://x/v1", "--background"},
			want: []string{"start", "--_bg"},
		},
		{
			name: "keeps other flags",
			args: []string{"discursive", "start", "--log-level", "debug", "--background"},
			want: []string{"start", "--log-level", "debug", "--_bg"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := backgroundChildArgs(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}
