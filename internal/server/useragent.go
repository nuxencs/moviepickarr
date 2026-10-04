package server

import "strings"

// The label is display copy only, never an auth or analytics input, so short
// hand-written token lists beat a full UA database dependency.

// deviceLabel turns a stored user agent into a "Safari on iPhone" label. Either
// half may be missing; with neither it returns "Unknown device".
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

// token maps a UA substring to a name. firstMatch takes the first hit, so list
// order is the disambiguation rule.
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
