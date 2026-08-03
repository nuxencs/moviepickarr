package server

import "strings"

// User-agent strings are a fiction every browser tells about itself: Chrome
// claims Safari, Edge claims Chrome, Opera claims both. So the matching below
// is ordered from most specific to least, and each list is short on purpose.
// The label is display copy for a member's own device list ("Safari on iPhone"),
// never an authorization or analytics input, so a wrong guess costs a slightly
// vague row and nothing else. That is also why there is no dependency here: a
// full UA database buys precision this surface has no use for.

// deviceLabel turns a stored user agent into the one line the session list
// shows. Either half may be unrecognized: an unknown browser falls back to the
// platform alone, an unknown platform to the browser alone, and neither to
// "Unknown device" (an API client, a curl, a stripped agent).
func deviceLabel(userAgent *string) string {
	if userAgent == nil || *userAgent == "" {
		return "Unknown device"
	}
	ua := *userAgent

	browser := firstMatch(ua, browserTokens)
	platform := firstMatch(ua, platformTokens)

	switch {
	case browser != "" && platform != "":
		return browser + " on " + platform
	case platform != "":
		return platform
	case browser != "":
		return browser
	default:
		return "Unknown device"
	}
}

// token is one substring to look for and the name to report when it is there.
// Both lists are ordered most-specific first, and firstMatch takes the first
// hit, so the ordering IS the disambiguation rule.
type token struct{ needle, name string }

func firstMatch(ua string, tokens []token) string {
	for _, t := range tokens {
		if strings.Contains(ua, t.needle) {
			return t.name
		}
	}
	return ""
}

// Edge, Samsung Internet, and Opera carry Chrome's token, and Chrome carries
// Safari's, so the impostors have to come before the originals.
var browserTokens = []token{
	{"EdgiOS/", "Edge"},
	{"EdgA/", "Edge"},
	{"Edg/", "Edge"},
	{"SamsungBrowser/", "Samsung Internet"},
	{"OPiOS/", "Opera"},
	{"OPR/", "Opera"},
	{"FxiOS/", "Firefox"},
	{"Firefox/", "Firefox"},
	{"CriOS/", "Chrome"},
	{"Chrome/", "Chrome"},
	{"Safari/", "Safari"},
}

// A ChromeOS agent says X11 and an Android one says Linux, so both have to come
// before the generic desktop tokens.
var platformTokens = []token{
	{"iPhone", "iPhone"},
	{"iPad", "iPad"},
	{"CrOS", "ChromeOS"},
	{"Android", "Android"},
	{"Macintosh", "macOS"},
	{"Mac OS X", "macOS"},
	{"Windows", "Windows"},
	{"Linux", "Linux"},
	{"X11", "Linux"},
}
