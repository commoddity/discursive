package doctor

import (
	"testing"

	doctpkg "github.com/commoddity/discursive/internal/doctor"
)

func TestCountFailed(t *testing.T) {
	tests := []struct {
		name   string
		report doctpkg.Report
		want   int
	}{
		{
			name: "all ok",
			report: doctpkg.Report{
				OK:     true,
				Checks: []doctpkg.Check{{Name: "a", OK: true}, {Name: "b", OK: true}},
			},
			want: 0,
		},
		{
			name: "some failed",
			report: doctpkg.Report{
				OK: false,
				Checks: []doctpkg.Check{
					{Name: "a", OK: true},
					{Name: "b", OK: false},
					{Name: "c", OK: false},
				},
			},
			want: 2,
		},
		{
			name: "all failed",
			report: doctpkg.Report{
				OK:     false,
				Checks: []doctpkg.Check{{Name: "a", OK: false}, {Name: "b", OK: false}},
			},
			want: 2,
		},
		{
			name: "empty",
			report: doctpkg.Report{
				OK:     true,
				Checks: nil,
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countFailed(tt.report)
			if got != tt.want {
				t.Fatalf("countFailed = %d want %d", got, tt.want)
			}
		})
	}
}
