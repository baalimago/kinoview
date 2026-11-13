package model

// ShowEpisode is an Item enriched with parsed show metadata.
type ShowEpisode struct {
	Item
	ShowName string `json:"showName"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
}

// ShowSeason groups episodes of the same season.
type ShowSeason struct {
	Season   int           `json:"season"`
	Episodes []ShowEpisode `json:"episodes"`
}

// ShowSeries groups seasons of the same show.
type ShowSeries struct {
	Name    string       `json:"name"`
	Seasons []ShowSeason `json:"seasons"`
}

// ShowsResponse is the top-level API response.
type ShowsResponse struct {
	Shows []ShowSeries `json:"shows"`
	// Movies are the video items that belong to no series, ordered by title.
	Movies []ShowMovie `json:"movies"`
}

// ShowMovie is an Item that belongs to no series: the movies view of the
// library browser renders it as one row.
type ShowMovie struct {
	Item
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}
