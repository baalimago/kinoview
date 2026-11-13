package model

import (
	"encoding/json"
	"testing"
)

// library returns the shape of the reference library: a movie whose file name
// is the bare title, sequels that share a prefix, and episodes whose
// descriptions mention unrelated words.
func library(t *testing.T) []Item {
	t.Helper()
	seinfeld := json.RawMessage(`{"name":"The Strike","showName":"Seinfeld","season":9,"episode":10,"description":"George refuses to celebrate Festivus."}`)
	saw := json.RawMessage(`{"name":"Saw","year":2004,"genre":"Horror","description":"Two men wake up chained in a room."}`)
	sawFour := json.RawMessage(`{"name":"Saw IV","year":2007,"genre":"Horror"}`)
	island := json.RawMessage(`{"name":"Shutter Island","year":2010,"genre":"Thriller"}`)
	return []Item{
		{ID: "aaa", Name: "Stargate.SG-1.S08E11.Gemini.mkv", Path: "/movies/stargate/Stargate.SG-1.S08E11.Gemini.mkv", MIMEType: "video/x-matroska", Metadata: &seinfeld},
		{ID: "bbb", Name: "4. Saw IV (2007) Unrated 1080p HighCode.mkv", Path: "/movies/saw/4. Saw IV (2007).mkv", MIMEType: "video/x-matroska", Metadata: &sawFour},
		{ID: "ccc", Name: "1. Saw (2004) 1080p HighCode.mkv", Path: "/movies/saw/1. Saw (2004).mkv", MIMEType: "video/x-matroska", Metadata: &saw},
		{ID: "ddd", Name: "Shutter Island 2010 2160p 4K.BluRay H265 10bit AAC 5.1.mkv", Path: "/movies/shutter/Shutter Island.mkv", MIMEType: "video/x-matroska", Metadata: &island},
	}
}

func TestRankItems(t *testing.T) {
	t.Parallel()

	t.Run("exact title outranks a longer title sharing the token", func(t *testing.T) {
		ranked := RankItems(library(t), "saw")
		if len(ranked) != 2 {
			t.Fatalf("expected 2 matches, got %d", len(ranked))
		}
		if ranked[0].ID != "ccc" {
			t.Fatalf("expected the bare 'Saw' movie first, got %q", ranked[0].Name)
		}
	})

	t.Run("metadata title outranks a path substring", func(t *testing.T) {
		ranked := RankItems(library(t), "shutter")
		if len(ranked) != 1 || ranked[0].ID != "ddd" {
			t.Fatalf("expected the Shutter Island movie, got %v", ranked)
		}
	})

	t.Run("all tokens must match, in any order", func(t *testing.T) {
		ranked := RankItems(library(t), "island shutter")
		if len(ranked) != 1 || ranked[0].ID != "ddd" {
			t.Fatalf("expected the Shutter Island movie, got %v", ranked)
		}
	})

	t.Run("the name token wins over a description token", func(t *testing.T) {
		festivus := json.RawMessage(`{"name":"Festivus","description":"About the made up holiday."}`)
		items := []Item{
			{ID: "ep", Name: "Seinfeld.S09E10.mkv", Metadata: &festivus},
			{ID: "movie", Name: "The Holiday 2006.mkv"},
		}
		ranked := RankItems(items, "holiday")
		if ranked[0].ID != "movie" {
			t.Fatalf("expected the movie named Holiday first, got %q", ranked[0].Name)
		}
	})

	t.Run("no match yields no items", func(t *testing.T) {
		if got := RankItems(library(t), "tarantino"); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})

	t.Run("punctuation does not separate the query from the name", func(t *testing.T) {
		md := json.RawMessage(`{"name":"Ocean's Eleven","year":2001}`)
		items := []Item{{ID: "o", Name: "Ocean's.Eleven.2001.mkv", Metadata: &md}}
		if got := RankItems(items, "oceans eleven"); len(got) != 1 {
			t.Fatalf("expected 'oceans eleven' to find Ocean's Eleven, got %v", got)
		}
	})

	t.Run("a blank query matches nothing to rank", func(t *testing.T) {
		if got := RankItems(library(t), "   "); len(got) != 0 {
			t.Fatalf("expected no matches for a blank query, got %v", got)
		}
		if !MatchesGlobalSearch(Item{ID: "x", Name: "saw.mkv"}, "   ") {
			t.Fatal("expected a blank needle to match every item")
		}
	})

	t.Run("ranked order is stable for equal scores", func(t *testing.T) {
		items := []Item{{ID: "b", Name: "Same.mkv"}, {ID: "a", Name: "same.mkv"}}
		first := RankItems(items, "same")
		second := RankItems(items, "same")
		if first[0].ID != second[0].ID {
			t.Fatalf("unstable order: %v vs %v", first, second)
		}
	})
}

func TestOrderItems(t *testing.T) {
	t.Parallel()

	t.Run("empty query orders by ID", func(t *testing.T) {
		ordered := OrderItems([]Item{{ID: "b"}, {ID: "a"}}, "")
		if ordered[0].ID != "a" {
			t.Fatalf("expected ID order, got %v", ordered)
		}
	})

	t.Run("query orders by relevance", func(t *testing.T) {
		ordered := OrderItems(library(t), "saw")
		if ordered[0].ID != "ccc" {
			t.Fatalf("expected relevance order, got %v", ordered)
		}
	})

	t.Run("does not reorder the caller's slice", func(t *testing.T) {
		items := []Item{{ID: "b"}, {ID: "a"}}
		OrderItems(items, "")
		if items[0].ID != "b" {
			t.Fatalf("expected the caller's slice to keep its order, got %v", items)
		}
	})
}

func TestMatchScore(t *testing.T) {
	t.Parallel()

	t.Run("year in metadata matches typed digits", func(t *testing.T) {
		md := json.RawMessage(`{"name":"Saw","year":2004}`)
		it := Item{ID: "x", Name: "saw.mkv", Metadata: &md}
		if MatchScore(it, "2004") == 0 {
			t.Fatal("expected the metadata year to match")
		}
	})

	t.Run("a missing token fails the whole query", func(t *testing.T) {
		it := Item{ID: "x", Name: "saw.mkv"}
		if MatchScore(it, "saw 2004") != 0 {
			t.Fatal("expected the query to fail when a token matches nothing")
		}
	})
}
