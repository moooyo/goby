package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/artwork"
)

func TestImageTransformWhitespaceDefaultsAndExplicitOverrides(t *testing.T) {
	for _, test := range []struct {
		name      string
		canonical string
		crop      bool
	}{
		{"Logo", "Logo", true},
		{"logo", "Logo", true},
		{"Art", "Art", true},
		{"ClearArt", "Art", true},
		{"clearart", "Art", true},
		{"Primary", "Primary", false},
		{"Backdrop", "Backdrop", false},
		{"Thumb", "Thumb", false},
		{"Banner", "Banner", false},
		{"Disc", "Disc", false},
		{"Box", "Box", false},
		{"BoxRear", "BoxRear", false},
		{"Menu", "Menu", false},
		{"Screenshot", "Screenshot", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := mustParseImageTransform(t, test.name, "")
			if request.typeName != test.canonical || request.options.CropWhitespace != test.crop {
				t.Fatalf("default type=%q crop=%t, want %q and %t", request.typeName, request.options.CropWhitespace, test.canonical, test.crop)
			}
			for _, query := range []string{"CropWhitespace=false", "cropwhitespace=0", "CROPWHITESPACE=FALSE"} {
				request = mustParseImageTransform(t, test.name, query)
				if request.options.CropWhitespace {
					t.Fatalf("explicit false was lost for %s", query)
				}
			}
			request = mustParseImageTransform(t, test.name, "CropWhitespace=true")
			if !request.options.CropWhitespace {
				t.Fatal("explicit whitespace cropping was lost")
			}
		})
	}
}

func TestImageTransformCropCoordinatesAndAliases(t *testing.T) {
	want := artwork.CropRect{X: 1, Y: 2, Width: 30, Height: 40}
	for _, query := range []string{
		"Crop=1,2,30,40",
		"CropX=1&CropY=2&CropWidth=30&CropHeight=40",
		"cropx=1&cropy=2&cropwidth=30&cropheight=40",
		"Crop=1,2,30,40&CropX=1&CropY=2&CropWidth=30&CropHeight=40",
	} {
		request := mustParseImageTransform(t, "Primary", query)
		if request.options.Crop != want {
			t.Errorf("crop for %q = %+v, want %+v", query, request.options.Crop, want)
		}
	}
	for _, query := range []string{
		"Crop=",
		"Crop=1,2,30",
		"Crop=1,2,30,40,50",
		"Crop=1,2,30,",
		"Crop=1.5,2,30,40",
		"Crop=-1,2,30,40",
		"Crop=1,-2,30,40",
		"Crop=1,2,0,40",
		"Crop=1,2,30,0",
		"Crop=1,2,-30,40",
		"Crop=16385,2,30,40",
		"Crop=1,16385,30,40",
		"Crop=1,2,16385,40",
		"Crop=1,2,30,16385",
		"CropX=1",
		"CropWidth=30&CropHeight=40",
		"CropX=1&CropY=2&CropWidth=30",
		"CropX=1&CropY=2&CropWidth=30&CropHeight=bad",
		"Crop=1,2,30,40&CropX=1",
		"Crop=1,2,30,40&CropX=2&CropY=2&CropWidth=30&CropHeight=40",
		"Crop=1,2,30,40&CropX=1&CropY=3&CropWidth=30&CropHeight=40",
		"Crop=1,2,30,40&CropX=1&CropY=2&CropWidth=31&CropHeight=40",
		"Crop=1,2,30,40&CropX=1&CropY=2&CropWidth=30&CropHeight=41",
	} {
		assertImageTransformRejected(t, "Primary", query)
	}
}

func TestImageTransformAnimationSwitchesAgree(t *testing.T) {
	for _, test := range []struct {
		query   string
		disable bool
	}{
		{"", false},
		{"KeepAnimation=true", false},
		{"KeepAnimation=false", true},
		{"DisableAnimation=true", true},
		{"DisableAnimation=false", false},
		{"KeepAnimation=true&DisableAnimation=false", false},
		{"KeepAnimation=false&DisableAnimation=true", true},
		{"KeepAnimation=0&DisableAnimation=1", true},
	} {
		request := mustParseImageTransform(t, "Primary", test.query)
		if request.options.DisableAnimation != test.disable {
			t.Errorf("DisableAnimation for %q = %t, want %t", test.query, request.options.DisableAnimation, test.disable)
		}
	}
	for _, query := range []string{
		"KeepAnimation=true&DisableAnimation=true",
		"KeepAnimation=false&DisableAnimation=false",
		"KeepAnimation=0&DisableAnimation=0",
		"KeepAnimation=1&DisableAnimation=1",
	} {
		assertImageTransformRejected(t, "Primary", query)
	}
}

func TestImageTransformColorsAndForegroundAreCanonical(t *testing.T) {
	for _, test := range []struct {
		query      string
		background string
		foreground string
	}{
		{"BackgroundColor=White", "#ffffffff", ""},
		{"BackgroundColor=black", "#000000ff", ""},
		{"BackgroundColor=transparent", "#00000000", ""},
		{"BackgroundColor=%23AaBBcC", "#aabbccff", ""},
		{"BackgroundColor=+%23AaBBcC80+", "#aabbcc80", ""},
		{"ForegroundLayer=PLAY", "", "play:#ffffffcc"},
		{"ForegroundLayer=music:%23AaBBcC", "", "music:#aabbccff"},
		{"ForegroundLayer=folder:black", "", "folder:#000000ff"},
		{"ForegroundLayer=play:transparent", "", "play:#00000000"},
		{"BackgroundColor=black&ForegroundLayer=+Music:%23Ff000080+", "#000000ff", "music:#ff000080"},
	} {
		request := mustParseImageTransform(t, "Primary", test.query)
		if request.options.BackgroundColor != test.background || request.options.ForegroundLayer != test.foreground {
			t.Errorf("query %q produced background=%q foreground=%q, want %q and %q", test.query,
				request.options.BackgroundColor, request.options.ForegroundLayer, test.background, test.foreground)
		}
	}
	for _, query := range []string{
		"BackgroundColor=red",
		"BackgroundColor=%23fff",
		"BackgroundColor=%2300000g",
		"BackgroundColor=ffffffff",
		"BackgroundColor=rgb(0,0,0)",
		"BackgroundColor=url(https://example.test/image)",
		"ForegroundLayer=unknown",
		"ForegroundLayer=play:",
		"ForegroundLayer=music:red",
		"ForegroundLayer=play:%23ffffff:extra",
		"ForegroundLayer=https://example.test/overlay.png",
		"ForegroundLayer=/tmp/overlay.png",
		"ForegroundLayer=data:image/png;base64,AAAA",
	} {
		assertImageTransformRejected(t, "Primary", query)
	}
}

func TestImageTransformNumericRanges(t *testing.T) {
	for _, test := range []struct {
		query   string
		percent float64
		count   int
	}{
		{"PercentPlayed=0", 0, 0},
		{"PercentPlayed=-0", 0, 0},
		{"PercentPlayed=0.25", 0.25, 0},
		{"PercentPlayed=99.999", 99.999, 0},
		{"PercentPlayed=100", 100, 0},
		{"PercentPlayed=1e2", 100, 0},
		{"UnplayedCount=0", 0, 0},
		{"UnplayedCount=1", 0, 1},
		{"UnplayedCount=9999", 0, 9999},
		{"PercentPlayed=25&UnplayedCount=12", 25, 12},
	} {
		request := mustParseImageTransform(t, "Primary", test.query)
		if request.options.PercentPlayed != test.percent || request.options.UnplayedCount != test.count {
			t.Errorf("query %q produced percent=%v count=%d, want %v and %d", test.query,
				request.options.PercentPlayed, request.options.UnplayedCount, test.percent, test.count)
		}
	}
	for _, query := range []string{
		"PercentPlayed=",
		"PercentPlayed=bad",
		"PercentPlayed=NaN",
		"PercentPlayed=Inf",
		"PercentPlayed=%2BInf",
		"PercentPlayed=-Inf",
		"PercentPlayed=1e309",
		"PercentPlayed=-0.001",
		"PercentPlayed=100.001",
		"UnplayedCount=",
		"UnplayedCount=bad",
		"UnplayedCount=-1",
		"UnplayedCount=10000",
		"UnplayedCount=1.0",
		"UnplayedCount=1e2",
		"UnplayedCount=NaN",
		"UnplayedCount=999999999999999999999999999999",
	} {
		assertImageTransformRejected(t, "Primary", query)
	}
}

func TestImageTransformRejectsMalformedSwitchesAndConflictingDuplicateKeys(t *testing.T) {
	for _, name := range []string{"CropWhitespace", "AutoOrient", "DisableAnimation", "KeepAnimation", "AddPlayedIndicator", "EnableImageEnhancers"} {
		for _, value := range []string{"", "yes", "no", "2", "false,true"} {
			assertImageTransformRejected(t, "Primary", name+"="+value)
		}
	}
	for _, query := range []string{
		"AutoOrient=true&AutoOrient=false",
		"AutoOrient=true&autoorient=false",
		"AutoOrient=true&autoorient=1",
		"Crop=1,2,30,40&crop=1,2,30,41",
		"BackgroundColor=white&backgroundcolor=black",
		"ForegroundLayer=play&foregroundlayer=music",
		"PercentPlayed=20&percentplayed=21",
		"UnplayedCount=2&unplayedcount=3",
		"Format=jpg&format=jpeg",
		"Tag=first&tag=second",
		"CropWhitespace=%zz",
	} {
		assertImageTransformRejected(t, "Primary", query)
	}
	for _, query := range []string{
		"AutoOrient=true&AutoOrient=true",
		"AutoOrient=true&autoorient=true",
		"Crop=1,2,30,40&crop=1,2,30,40",
		"PercentPlayed=20&percentplayed=20",
		"ForegroundLayer=play&foregroundlayer=play",
		"Tag=first&tag=first",
	} {
		mustParseImageTransform(t, "Primary", query)
	}
}

func TestImageTransformCacheIdentitiesDistinguishEveryEffect(t *testing.T) {
	const tag = "transform-source"
	variants := []string{
		"",
		"Format=png",
		"Format=gif",
		"Format=jpeg",
		"Width=128",
		"Height=128",
		"MaxWidth=128",
		"MaxHeight=128",
		"Quality=90",
		"Crop=1,2,30,40",
		"Crop=2,2,30,40",
		"Crop=1,3,30,40",
		"Crop=1,2,31,40",
		"Crop=1,2,30,41",
		"CropWhitespace=true",
		"AutoOrient=true",
		"DisableAnimation=true",
		"BackgroundColor=white",
		"BackgroundColor=black",
		"ForegroundLayer=play",
		"ForegroundLayer=music",
		"ForegroundLayer=folder",
		"ForegroundLayer=play:black",
		"AddPlayedIndicator=true",
		"PercentPlayed=25",
		"PercentPlayed=50",
		"UnplayedCount=1",
		"UnplayedCount=2",
		"PercentPlayed=25&UnplayedCount=1&AddPlayedIndicator=true",
	}
	keys, etags := map[string]string{}, map[string]string{}
	for _, query := range variants {
		options := mustParseImageTransform(t, "Primary", query).options
		key := imageVariantKey(tag, options)
		etag := imageETag(tag, options)
		if !strings.HasPrefix(key, tag+"/"+artwork.TransformationVersion+"/") {
			t.Errorf("cache key %q omits the algorithm version namespace", key)
		}
		if previous, ok := keys[key]; ok {
			t.Errorf("different effects %q and %q share cache key %q", previous, query, key)
		}
		if previous, ok := etags[etag]; ok {
			t.Errorf("different effects %q and %q share validator %q", previous, query, etag)
		}
		keys[key], etags[etag] = query, query
		if imageVariantKey("changed-source", options) == key || imageETag("changed-source", options) == etag {
			t.Errorf("changed source kept a derivative identity for %q", query)
		}
	}
	if etag := imageETag(tag, artwork.Options{}); etag != `"transform-source"` {
		t.Fatalf("unchanged request validator = %q, want the quoted original tag", etag)
	}
}

func TestImageTransformAliasesShareCanonicalCacheIdentities(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "Format=original"},
		{"", "KeepAnimation=true"},
		{"", "DisableAnimation=false"},
		{"", "PercentPlayed=-0&UnplayedCount=0"},
		{"", "EnableImageEnhancers=true"},
		{"", "EnableImageEnhancers=false"},
		{"Format=jpg", "format=JPEG"},
		{"Crop=1,2,30,40", "CropX=1&CropY=2&CropWidth=30&CropHeight=40"},
		{"CropWhitespace=true", "cropwhitespace=1"},
		{"AutoOrient=true", "AUTOORIENT=TRUE"},
		{"DisableAnimation=true", "KeepAnimation=false"},
		{"DisableAnimation=true", "KeepAnimation=false&DisableAnimation=true"},
		{"BackgroundColor=white", "BackgroundColor=%23FFFFFF"},
		{"BackgroundColor=black", "BackgroundColor=%23000000FF"},
		{"BackgroundColor=transparent", "BackgroundColor=%2300000000"},
		{"ForegroundLayer=play", "ForegroundLayer=PLAY:%23FFFFFFCC"},
		{"ForegroundLayer=music:white", "ForegroundLayer=music:%23ffffffff"},
		{"AddPlayedIndicator=true", "AddPlayedIndicator=1"},
		{"PercentPlayed=10", "PercentPlayed=1e1"},
		{"UnplayedCount=2", "UnplayedCount=002"},
	} {
		first := mustParseImageTransform(t, "Primary", pair[0]).options
		second := mustParseImageTransform(t, "Primary", pair[1]).options
		if first != second {
			t.Errorf("aliases %q and %q do not produce equal canonical options: %+v and %+v", pair[0], pair[1], first, second)
		}
		if imageVariantKey("source", first) != imageVariantKey("source", second) || imageETag("source", first) != imageETag("source", second) {
			t.Errorf("aliases %q and %q have different cache identities", pair[0], pair[1])
		}
	}
	art := mustParseImageTransform(t, "Art", "")
	clearArt := mustParseImageTransform(t, "ClearArt", "")
	if art != clearArt || imageVariantKey("source", art.options) != imageVariantKey("source", clearArt.options) {
		t.Fatal("Art and ClearArt aliases have different canonical requests or cache identities")
	}
}

func mustParseImageTransform(t *testing.T, imageType, query string) imageRequest {
	t.Helper()
	request := httptest.NewRequest("GET", "/emby/Items/item/Images/"+imageType+"?"+query, nil)
	request.SetPathValue("Type", imageType)
	parsed, err := parseImageRequest(request)
	if err != nil {
		t.Fatalf("parse %q for %q: %v", query, imageType, err)
	}
	return parsed
}

func assertImageTransformRejected(t *testing.T, imageType, query string) {
	t.Helper()
	request := httptest.NewRequest("GET", "/emby/Items/item/Images/"+imageType+"?"+query, nil)
	request.SetPathValue("Type", imageType)
	if _, err := parseImageRequest(request); err == nil {
		t.Errorf("accepted malformed transformation query %q for %q", query, imageType)
	}
}
