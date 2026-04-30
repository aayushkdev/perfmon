package intel

import "testing"

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []int
	}{
		{name: "single", in: "4", want: []int{4}},
		{name: "range", in: "0-3", want: []int{0, 1, 2, 3}},
		{name: "mixed", in: "0-1,4,8-10", want: []int{0, 1, 4, 8, 9, 10}},
		{name: "ignores invalid", in: "1,nope,4-2", want: []int{1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCPUList(tt.in)
			for _, id := range tt.want {
				if !got[id] {
					t.Fatalf("expected CPU %d in parsed set for %q", id, tt.in)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d CPUs, want %d: %#v", len(got), len(tt.want), got)
			}
		})
	}
}
