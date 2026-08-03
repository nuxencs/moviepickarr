package server

import "testing"

func TestDeviceLabel(t *testing.T) {
	strptr := func(s string) *string { return &s }

	tests := []struct {
		name string
		ua   *string
		want string
	}{
		{
			name: "chrome on macos",
			ua:   strptr("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"),
			want: "Chrome on macOS",
		},
		{
			name: "safari on iphone",
			ua:   strptr("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"),
			want: "Safari on iPhone",
		},
		{
			name: "chrome on iphone",
			ua:   strptr("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.6478.108 Mobile/15E148 Safari/604.1"),
			want: "Chrome on iPhone",
		},
		{
			name: "firefox on iphone",
			ua:   strptr("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/127.0 Mobile/15E148 Safari/605.1.15"),
			want: "Firefox on iPhone",
		},
		{
			name: "edge on iphone",
			ua:   strptr("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) EdgiOS/126.0 Mobile/15E148 Safari/605.1.15"),
			want: "Edge on iPhone",
		},
		{
			name: "safari on ipad",
			ua:   strptr("Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/604.1"),
			want: "Safari on iPad",
		},
		{
			name: "safari on macos",
			ua:   strptr("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4.1 Safari/605.1.15"),
			want: "Safari on macOS",
		},
		{
			name: "firefox on windows",
			ua:   strptr("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 Firefox/127.0"),
			want: "Firefox on Windows",
		},
		{
			name: "firefox on linux",
			ua:   strptr("Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0"),
			want: "Firefox on Linux",
		},
		{
			// Edge ships Chrome's token too, so the Edg check has to win.
			name: "edge on windows",
			ua:   strptr("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0"),
			want: "Edge on Windows",
		},
		{
			name: "edge on android",
			ua:   strptr("Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36 EdgA/126.0.0.0"),
			want: "Edge on Android",
		},
		{
			name: "samsung internet on android",
			ua:   strptr("Mozilla/5.0 (Linux; Android 14; SM-S921B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36"),
			want: "Samsung Internet on Android",
		},
		{
			// Opera ships both Chrome and Safari tokens.
			name: "opera on macos",
			ua:   strptr("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 OPR/111.0.0.0"),
			want: "Opera on macOS",
		},
		{
			name: "chrome on android",
			ua:   strptr("Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"),
			want: "Chrome on Android",
		},
		{
			name: "chromeos reports itself, not the linux underneath",
			ua:   strptr("Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"),
			want: "Chrome on ChromeOS",
		},
		{
			name: "known os, unrecognized browser falls back to the os alone",
			ua:   strptr("SomeBot/1.0 (Windows NT 10.0)"),
			want: "Windows",
		},
		{
			name: "known browser, unrecognized os falls back to the browser alone",
			ua:   strptr("Mozilla/5.0 (Unknown) Firefox/127.0"),
			want: "Firefox",
		},
		{
			name: "unrecognized agent",
			ua:   strptr("curl/8.4.0"),
			want: "Unknown device",
		},
		{
			name: "empty agent",
			ua:   strptr(""),
			want: "Unknown device",
		},
		{
			name: "missing agent",
			ua:   nil,
			want: "Unknown device",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deviceLabel(tt.ua); got != tt.want {
				t.Errorf("deviceLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}
