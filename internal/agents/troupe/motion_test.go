package troupe

import (
	"slices"
	"testing"
)

// motionPlay builds one play spec for the pure comparison: one backdrop with
// no entry (it only stands there) and one actor entry list.
func motionPlay(backdropScale float64, entries []TimelineEntry) PlaySpec {
	return PlaySpec{
		Instances: []PlayInstance{
			{ID: "forest", Model: Ref("forest@1"), Role: "backdrop", Scale: backdropScale},
			{ID: "cat", Model: Ref("cat@1"), Role: "actor", Scale: 1, X: new(0.1), Y: new(float64(0))},
		},
		Timeline: entries,
	}
}

// TestRepeatedInstances pins the comparison: an instance repeats only when its
// model, its placement and its motion entries are identical, entry for entry.
// A new set behind the same performance is a repeat; a backdrop with no entry
// can never repeat.
func TestRepeatedInstances(t *testing.T) {
	walk := TimelineEntry{At: 0, On: "cat", Clip: new(Ref("walk@1"))}
	recede := TimelineEntry{At: 0, On: "cat", Tween: &Tween{To: TweenTarget{X: new(0.5)}, Over: 3000, Easing: EasingLinear}}

	cases := []struct {
		name string
		prev PlaySpec
		next PlaySpec
		want []string
	}{
		{
			name: "identical motion behind a new set",
			prev: motionPlay(2.4, []TimelineEntry{walk, recede}),
			next: motionPlay(3.1, []TimelineEntry{walk, recede}),
			want: []string{"cat"},
		},
		{
			name: "retimed entry",
			prev: motionPlay(2.4, []TimelineEntry{walk, recede}),
			next: motionPlay(2.4, []TimelineEntry{walk, {At: 0, On: "cat", Tween: &Tween{To: TweenTarget{X: new(0.5)}, Over: 3200, Easing: EasingLinear}}}),
			want: nil,
		},
		{
			name: "different clip version",
			prev: motionPlay(2.4, []TimelineEntry{walk, recede}),
			next: motionPlay(2.4, []TimelineEntry{{At: 0, On: "cat", Clip: new(Ref("walk@2"))}, recede}),
			want: nil,
		},
		{
			name: "moved placement",
			prev: motionPlay(2.4, []TimelineEntry{walk, recede}),
			next: func() PlaySpec {
				s := motionPlay(2.4, []TimelineEntry{walk, recede})
				s.Instances[1].X = new(0.3)
				return s
			}(),
			want: nil,
		},
		{
			name: "extra entry appended",
			prev: motionPlay(2.4, []TimelineEntry{walk, recede}),
			next: motionPlay(2.4, []TimelineEntry{walk, recede, {At: 3000, On: "cat", Clip: new(Ref("blink@1"))}}),
			want: nil,
		},
		{
			name: "the first film repeats nothing",
			prev: PlaySpec{},
			next: motionPlay(2.4, []TimelineEntry{walk, recede}),
			want: nil,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := repeatedInstances(tt.prev, tt.next)
			if !slices.Equal(got, tt.want) {
				t.Errorf("repeated = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRepeatedMotion_FromWorktree pins the play-level seam: the fact is read
// from two submitted plays in the worktree, so the critic is told the repeat
// rather than inferring it from prose.
func TestRepeatedMotion_FromWorktree(t *testing.T) {
	s := newTestSubmitter(t)
	first, second := "story_20260821T093000Z", "story_20260821T093100Z"
	writeDraft(t, s.worktree, first)
	if _, err := s.Submit(first); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	writeDraft(t, s.worktree, second) // the same performance, a new id
	if _, err := s.Submit(second); err != nil {
		t.Fatalf("second submit: %v", err)
	}

	lib, err := NewPlayLibrary(s.worktree)
	if err != nil {
		t.Fatalf("NewPlayLibrary: %v", err)
	}
	if got, err := repeatedMotion(lib, second); err != nil || !slices.Equal(got, []string{"cat"}) {
		t.Errorf("repeated = %v (err %v), want [cat]", got, err)
	}
	if got, err := repeatedMotion(lib, first); err != nil || got != nil {
		t.Errorf("the first submitted play repeated %v (err %v), want nothing", got, err)
	}
}

// TestRepeatedMotion_UnknownPlay pins the read-only seam's tolerance: an id
// with no play file reports no repeat instead of failing the review.
func TestRepeatedMotion_UnknownPlay(t *testing.T) {
	lib, err := NewPlayLibrary(seedWorktree(t))
	if err != nil {
		t.Fatalf("NewPlayLibrary: %v", err)
	}
	if got, err := repeatedMotion(lib, "story_20260822T000000Z"); err != nil || got != nil {
		t.Errorf("repeated = %v (err %v), want nothing", got, err)
	}
}
