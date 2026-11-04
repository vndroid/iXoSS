package main

import (
	"encoding/json"
	"reflect"
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

func TestNonOutputSettingsKeepCache(t *testing.T) {
	pc := processing.NewDefaultConfig()
	baseline, err := cacheVariant(pc, vips.NewDefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"IXOSS_VIPS_LEAK_CHECK",
		"IXOSS_VIPS_CACHE_TRACE",
		"IXOSS_TIFF_UNLIMITED",
		"IXOSS_SVG_UNLIMITED",
		"IXOSS_PNG_UNLIMITED", // metadata is stripped by default
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "true")
			vc := vips.NewDefaultConfig()
			if _, err := vips.LoadConfigFromEnv(&vc); err != nil {
				t.Fatal(err)
			}
			variant, err := cacheVariant(pc, vc)
			if err != nil {
				t.Fatal(err)
			}
			if variant != baseline {
				t.Fatal("a setting that cannot change output changed the cache identity")
			}
		})
	}
}

// Without the PNG limit every text chunk is read, not only the first 20, and
// kept metadata carries them into PNG output.
func TestPNGLimitKeyedWhenMetadataKept(t *testing.T) {
	pc, vc := processing.NewDefaultConfig(), vips.NewDefaultConfig()
	pc.StripMetadata = false
	limited, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	vc.PngUnlimited = true
	unlimited, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	if limited == unlimited {
		t.Fatal("IXOSS_PNG_UNLIMITED with kept metadata reused the cache identity")
	}
}

// Clearing settings must not move default deployments to new keys.
func TestDefaultCacheIdentityUnchanged(t *testing.T) {
	pc, vc := processing.NewDefaultConfig(), vips.NewDefaultConfig()
	previous, err := json.Marshal(struct {
		Processing processing.Config
		Vips       vips.Config
	}{Processing: pc, Vips: vc})
	if err != nil {
		t.Fatal(err)
	}
	variant, err := cacheVariant(pc, vc)
	if err != nil {
		t.Fatal(err)
	}
	if variant != string(previous) {
		t.Fatal("default cache identity changed; existing caches would all miss")
	}
}

// Every vips.Config field has to be classified, so a new loader or debug
// option cannot silently start (or stop) splitting the cache.
func TestVipsConfigFieldsClassified(t *testing.T) {
	known := map[string]bool{
		// Output-affecting: always part of the identity.
		"JpegProgressive": true, "PngInterlaced": true, "PngQuantize": true,
		"PngQuantizationColors": true, "WebpPreset": true, "AvifSpeed": true,
		"WebpEffort": true, "JxlEffort": true,
		// Cleared in cacheVariant (PngUnlimited only while metadata is stripped).
		"PngUnlimited": true, "SvgUnlimited": true, "TiffUnlimited": true,
		"LeakCheck": true, "CacheTrace": true,
	}
	typ := reflect.TypeFor[vips.Config]()
	for i := range typ.NumField() {
		if name := typ.Field(i).Name; !known[name] {
			t.Errorf("vips.Config.%s is not classified for the cache identity", name)
		}
	}
}
