package ossprocess

import (
	"testing"

	"github.com/vndroid/iXoSS/internal/imagetype"
)

func TestMatchesOriginal(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"image/resize,w_700", true},
		{"image/resize,h_500", true},
		{"image/resize,w_700,h_500", true},
		{"image/resize,w_700,h_600", true},
		{"image/resize,m_fill,w_700,h_500", true},
		{"image/resize,m_pad,w_700,h_500", true},
		{"image/resize,m_mfit,w_700,h_500", true},
		{"image/resize,l_700", true},
		{"image/resize,s_500", true},
		{"image/resize,p_100", true},
		{"image/resize,w_1400", true},
		{"image/resize,w_1400,limit_0", false},
		{"image/resize,w_350", false},
		{"image/resize,m_fill,w_700,h_400", false},
		{"image/resize,m_pad,w_700,h_600", false},
		{"image/resize,m_fixed,w_700,h_600,limit_0", false},
		{"image/resize,w_700/format,jpg", true},
		{"image/format,jpeg", true},
		{"image/format,f_jpg", true},
		{"image/format,gif", true},
		{"image/format,png", false},
		{"image/resize,w_700/quality,q_80", false},
		{"image/resize,w_700/rotate,0", false},
		{"image/resize,w_700/crop,w_700,h_500", false},
	} {
		chain, err := Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := chain.Validate(); err != nil {
			t.Fatal(err)
		}
		if got := chain.MatchesOriginal(700, 500, imagetype.JPEG, imagetype.Unknown); got != tc.want {
			t.Errorf("%s: matches=%t want=%t", tc.raw, got, tc.want)
		}
	}
	chain, _ := Parse("image/resize,w_700/format,auto")
	for _, format := range []imagetype.Type{imagetype.Unknown, imagetype.JPEG, imagetype.WEBP} {
		if got := chain.MatchesOriginal(700, 500, imagetype.JPEG, format); got != (format != imagetype.WEBP) {
			t.Errorf("auto %s: matches=%t", format, got)
		}
	}
	if chain.MatchesOriginal(0, 0, imagetype.JPEG, imagetype.Unknown) {
		t.Fatal("unknown dimensions must not qualify for resize passthrough")
	}
}
