package engine

import "testing"

// Shared-link query parameters must not turn a single short-form video into a
// different request, nor be forwarded as cookies/tracking/playlist controls.
func TestCanonicalShortFormURLs(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"youtube-short", "https://www.youtube.com/shorts/18NGQq7p3LY?si=tracking&feature=share", "https://www.youtube.com/shorts/18NGQq7p3LY"},
		{"mobile-short", "https://m.youtube.com/shorts/BGQWPY4IigY#share", "https://m.youtube.com/shorts/BGQWPY4IigY"},
		{"youtu-be", "https://youtu.be/18NGQq7p3LY?si=tracking&t=15", "https://youtu.be/18NGQq7p3LY"},
		{"watch-individual", "https://www.youtube.com/watch?v=18NGQq7p3LY&list=PL-unrelated&index=2", "https://www.youtube.com/watch?v=18NGQq7p3LY"},
		{"embed", "https://www.youtube.com/embed/18NGQq7p3LY?autoplay=1", "https://www.youtube.com/embed/18NGQq7p3LY"},
		{"instagram-reel", "https://www.instagram.com/reel/Chunk8-jurw/?igsh=tracking", "https://www.instagram.com/reel/Chunk8-jurw/"},
		{"instagram-reels", "https://www.instagram.com/reels/Cop84x6u7CP/?utm_source=ig_web_copy_link", "https://www.instagram.com/reels/Cop84x6u7CP/"},
		{"instagram-user-reel", "https://www.instagram.com/marvelskies.fc/reel/CWqAgUZgCku/?igsh=tracking", "https://www.instagram.com/marvelskies.fc/reel/CWqAgUZgCku/"},
		{"tiktok-short", "https://vm.tiktok.com/ZExample/?is_from_webapp=1", "https://vm.tiktok.com/ZExample/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalSourceURL(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("CanonicalSourceURL(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
			}
			again, err := CanonicalSourceURL(got)
			if err != nil || again != got {
				t.Fatalf("canonical short-form URL is not stable: %q, %v", again, err)
			}
		})
	}
}

func TestShortFormNormalizationDoesNotRelaxSourcePolicy(t *testing.T) {
	for _, raw := range []string{
		"http://www.youtube.com/shorts/18NGQq7p3LY",
		"https://www.youtube.com.evil.test/shorts/18NGQq7p3LY",
		"https://user@www.youtube.com/shorts/18NGQq7p3LY",
		"https://www.youtube.com/shorts/18NGQq7p3LY%2f..%2fsecret",
		"https://www.youtube.com/shorts/18NGQq7p3LY%5csecret",
		"https://www.youtube.com:8443/shorts/18NGQq7p3LY",
		"https://www.youtube.com/watch?v=18NGQq7p3LY&v=BGQWPY4IigY",
		"https://www.instagram.com/reel/Chunk8-jurw/?igsh=%zz",
	} {
		t.Run(raw, func(t *testing.T) {
			if got, err := CanonicalSourceURL(raw); err == nil {
				t.Fatalf("unsafe short-form source accepted: %q", got)
			}
		})
	}
}
