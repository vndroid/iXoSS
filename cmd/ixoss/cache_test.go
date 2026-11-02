package main

import (
	"testing"

	"github.com/vndroid/iXoSS/internal/processing"
	"github.com/vndroid/iXoSS/internal/vips"
)

func TestEncoderSettingsInvalidateCache(t *testing.T) {
	pc := processing.NewDefaultConfig()
	baseline, err := cacheVariant(pc, vips.NewDefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []struct{ name, value string }{
		{"IXOSS_JPEG_PROGRESSIVE", "true"},
		{"IXOSS_PNG_INTERLACED", "true"},
		{"IXOSS_PNG_QUANTIZE", "true"},
		{"IXOSS_PNG_QUANTIZATION_COLORS", "128"},
		{"IXOSS_AVIF_SPEED", "6"},
		{"IXOSS_WEBP_EFFORT", "6"},
		{"IXOSS_WEBP_PRESET", "photo"},
		{"IXOSS_JXL_EFFORT", "6"},
	} {
		t.Run(setting.name, func(t *testing.T) {
			t.Setenv(setting.name, setting.value)
			vc := vips.NewDefaultConfig()
			if _, err := vips.LoadConfigFromEnv(&vc); err != nil {
				t.Fatal(err)
			}
			variant, err := cacheVariant(pc, vc)
			if err != nil {
				t.Fatal(err)
			}
			if variant == baseline {
				t.Fatal("changed encoder setting reused the cache identity")
			}
		})
	}
}

func TestProcessingSettingsInvalidateCache(t *testing.T) {
	pc, vc := processing.NewDefaultConfig(), vips.NewDefaultConfig()
	baseline, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	pc.Quality--
	changed, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	if changed == baseline {
		t.Fatal("changed processing quality reused the cache identity")
	}
	pc.Quality++
	restored, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	if restored != baseline {
		t.Fatal("restored settings changed the cache identity")
	}
}
