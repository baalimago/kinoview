package model

type MediaInfo struct {
	Streams []Stream `json:"streams"`
}

type Stream struct {
	Index              int         `json:"index"`
	CodecName          string      `json:"codec_name"`
	CodecLongName      string      `json:"codec_long_name"`
	Profile            string      `json:"profile,omitempty"`
	CodecType          string      `json:"codec_type"`
	CodecTagString     string      `json:"codec_tag_string"`
	CodecTag           string      `json:"codec_tag"`
	Width              int         `json:"width,omitempty"`
	Height             int         `json:"height,omitempty"`
	CodedWidth         int         `json:"coded_width,omitempty"`
	CodedHeight        int         `json:"coded_height,omitempty"`
	ClosedCaptions     int         `json:"closed_captions,omitempty"`
	HasBFrames         int         `json:"has_b_frames,omitempty"`
	SampleAspectRatio  string      `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string      `json:"display_aspect_ratio,omitempty"`
	PixFmt             string      `json:"pix_fmt,omitempty"`
	Level              int         `json:"level,omitempty"`
	ColorRange         string      `json:"color_range,omitempty"`
	ChromaLocation     string      `json:"chroma_location,omitempty"`
	Refs               int         `json:"refs,omitempty"`
	SampleFmt          string      `json:"sample_fmt,omitempty"`
	SampleRate         string      `json:"sample_rate,omitempty"`
	Channels           int         `json:"channels,omitempty"`
	ChannelLayout      string      `json:"channel_layout,omitempty"`
	BitsPerSample      int         `json:"bits_per_sample,omitempty"`
	RFrameRate         string      `json:"r_frame_rate"`
	AvgFrameRate       string      `json:"avg_frame_rate"`
	TimeBase           string      `json:"time_base"`
	StartPts           int         `json:"start_pts"`
	StartTime          string      `json:"start_time"`
	DurationTS         int64       `json:"duration_ts,omitempty"`
	Duration           string      `json:"duration,omitempty"`
	Disposition        Disposition `json:"disposition"`
	Tags               Tags        `json:"tags"`
}

type Disposition struct {
	Default         int `json:"default"`
	Dub             int `json:"dub"`
	Original        int `json:"original"`
	Comment         int `json:"comment"`
	Lyrics          int `json:"lyrics"`
	Karaoke         int `json:"karaoke"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	CleanEffects    int `json:"clean_effects"`
	AttachedPic     int `json:"attached_pic"`
	TimedThumbnails int `json:"timed_thumbnails"`
}

type Tags struct {
	Title                    string `json:"title,omitempty"`
	Language                 string `json:"language,omitempty"`
	BPS                      string `json:"BPS,omitempty"`
	Duration                 string `json:"DURATION,omitempty"`
	NumberOfFrames           string `json:"NUMBER_OF_FRAMES,omitempty"`
	NumberOfBytes            string `json:"NUMBER_OF_BYTES,omitempty"`
	StatisticsWritingApp     string `json:"_STATISTICS_WRITING_APP,omitempty"`
	StatisticsWritingDateUTC string `json:"_STATISTICS_WRITING_DATE_UTC,omitempty"`
	StatisticsTags           string `json:"_STATISTICS_TAGS,omitempty"`
}
