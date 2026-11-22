package app

import (
	"testing"

	"pitta/internal/api"
)

// The tally is the whole arithmetic of the stage bar, and the only part of it
// that can be wrong without looking wrong: a pipeline that broke at stage three
// must read as "3/4, one failed", not "2/4 still going".
func TestStageTally(t *testing.T) {
	cases := []struct {
		name       string
		states     []string
		wantDone   int
		wantTotal  int
		wantFailed int
	}{
		{"no children", nil, 0, 0, 0},
		{"nothing started", []string{"pending", "queued"}, 0, 2, 0},
		{"one running", []string{"done", "running"}, 1, 2, 0},
		{"a failure is spent", []string{"done", "failed", "queued"}, 2, 3, 1},
		{"serialized finish states", []string{"done", "finished", "terminal"}, 3, 3, 0},
		{"a stopped stage is spent, not broken", []string{"aborted", "canceled"}, 2, 2, 0},
		{"an unknown state is not counted as done", []string{"exploded"}, 0, 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			children := make([]api.TaskSubAgent, len(c.states))
			for i, state := range c.states {
				children[i] = api.TaskSubAgent{State: state}
			}
			done, total, failed := stageTally(children)
			if done != c.wantDone || total != c.wantTotal || failed != c.wantFailed {
				t.Fatalf("got %d/%d (failed %d), want %d/%d (failed %d)",
					done, total, failed, c.wantDone, c.wantTotal, c.wantFailed)
			}
		})
	}
}
