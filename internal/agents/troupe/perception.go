package troupe

import "fmt"

// showPartial is the shared inspection contract every agent prompt carries
// when the perception tools are enabled: the show renders in a browser at one
// URL, and the troupe looks at what it authored before it calls the work
// done. The URL is substituted, so the model never guesses the address.
func showPartial(showURL string) string {
	return fmt.Sprintf(`SHOW
The show renders in a browser at %s. The server renders the newest submitted
play.
To inspect: navigate to the URL with the browser tools, wait for the play
to run, read the outline for the exact wording, take a screenshot and look at
it, and read the browser console for errors. Use view_image when an inspection
leaves an image file. Close the browser when you finish.
`, showURL)
}
