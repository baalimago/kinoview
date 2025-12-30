package constants

const MetadataFormat = `{
	"name": "<NAME>",
	"alt_name": "<ALTERNATIVE NAME (if there were multiple titles)>" (string),
	"actors": [ "ACTOR FULLNAME 0" (string), "ACTOR FULLNAME 1" (string), ... ],
	"year": <RELEASE YEAR OF MEDIA> (int),
	"description": "<DESCRIPTION OF MEDIA (max 100 words)>" (string),
	"langugae": "<LANGUAGE (primarily spoken language)>" (string),
	"duration_min": <DURATION OF MEDIA IN MINUTES> (int),
	"season": <SEASON (if series)> (int),
	"episode": <EPISODE NUMBER (if series)> (int),
	"extra_to": "<MAIN MEDIA NAME (if extras, such as behind the scenes)>" (string)
}`
