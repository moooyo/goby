package media

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestEmbeddedPictureTypeUsesExactLabelsAndCommentPrecedence(t *testing.T) {
	for _, test := range []struct {
		name    string
		comment string
		title   string
		want    string
	}{
		{name: "front comment", comment: "Cover (front)", want: "Front"},
		{name: "front cover comment", comment: "front cover", want: "Front"},
		{name: "front short comment", comment: "front", want: "Front"},
		{name: "front numeric comment", comment: "3", want: "Front"},
		{name: "back comment", comment: "Cover (back)", want: "Back"},
		{name: "back cover comment", comment: "back cover", want: "Back"},
		{name: "back short comment", comment: "back", want: "Back"},
		{name: "back numeric comment", comment: "4", want: "Back"},
		{name: "normalized comment", comment: " \tCoVeR (FrOnT)\r\n", want: "Front"},
		{name: "front title fallback", title: " \tFRONT COVER ", want: "Front"},
		{name: "back title fallback", comment: "booklet", title: "4", want: "Back"},
		{name: "blank comment title fallback", comment: " \t", title: "back", want: "Back"},
		{name: "front comment overrides back title", comment: "3", title: "Back", want: "Front"},
		{name: "back comment overrides front title", comment: "Cover (back)", title: "Front", want: "Back"},
		{name: "other comment overrides front title", comment: "Other", title: "Front", want: "Other"},
		{name: "normalized other comment overrides title", comment: " \tOtHeR ", title: "Back", want: "Other"},
		{name: "missing tags", want: "Other"},
		{name: "generic cover", comment: "Cover", title: "Album artwork", want: "Other"},
		{name: "other numeric type", comment: "5", title: "Booklet", want: "Other"},
		{name: "filename is not a type", comment: "front.jpg", title: "back.png", want: "Other"},
		{name: "substring is not a type", comment: "not a front cover", title: "back cover scan", want: "Other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := embeddedPictureType(test.comment, test.title); got != test.want {
				t.Fatalf("embeddedPictureType(%q, %q) = %q; want %q", test.comment, test.title, got, test.want)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsOrdersByPictureTypeAndAbsoluteIndex(t *testing.T) {
	data := []byte(`{"streams":[
		{"index":31,"codec_name":"mjpeg","codec_type":"video","width":31,"height":19,"disposition":{"attached_pic":1},"tags":{"comment":"Cover (back)","title":"Front"}},
		{"index":17,"codec_name":"gif","codec_type":"video","width":17,"height":19,"disposition":{"attached_pic":1},"tags":{"comment":"booklet","title":"Front cover"}},
		{"index":0,"codec_name":"flac","codec_type":"audio"},
		{"index":11,"codec_name":"png","codec_type":"video","width":11,"height":19,"disposition":{"attached_pic":1},"tags":{"title":"Leaflet"}},
		{"index":2,"codec_name":"png","codec_type":"video","width":2,"height":19,"disposition":{"attached_pic":1},"tags":{"CoMmEnT":" FRONT ","TITLE":"Back"}},
		{"index":5,"codec_name":"h264","codec_type":"video","width":1920,"height":1080,"disposition":{"attached_pic":0},"tags":{"comment":"Cover (front)"}},
		{"index":4,"codec_name":"mjpeg","codec_type":"video","width":4,"height":19,"disposition":{"attached_pic":1},"tags":{"title":"4"}},
		{"index":8,"codec_name":"gif","codec_type":"video","width":8,"height":19,"disposition":{"attached_pic":1}}
	]}`)
	catalog := map[int]Stream{
		2:  {Index: 2, Codec: "png", CodecType: "video", Width: 2, Height: 19, IsAttachedPicture: true},
		4:  {Index: 4, Codec: "mjpeg", CodecType: "video", Width: 4, Height: 19, IsAttachedPicture: true},
		8:  {Index: 8, Codec: "gif", CodecType: "video", Width: 8, Height: 19, IsAttachedPicture: true},
		11: {Index: 11, Codec: "png", CodecType: "video", Width: 11, Height: 19, IsAttachedPicture: true},
		17: {Index: 17, Codec: "gif", CodecType: "video", Width: 17, Height: 19, IsAttachedPicture: true},
		31: {Index: 31, Codec: "mjpeg", CodecType: "video", Width: 31, Height: 19, IsAttachedPicture: true},
	}
	got, err := embeddedArtworkStreams(data, catalog)
	if err != nil {
		t.Fatalf("parse unordered picture metadata: %v", err)
	}
	want := []EmbeddedPicture{
		{StreamIndex: 2, PictureType: "Front"},
		{StreamIndex: 17, PictureType: "Front"},
		{StreamIndex: 8, PictureType: "Other"},
		{StreamIndex: 11, PictureType: "Other"},
		{StreamIndex: 4, PictureType: "Back"},
		{StreamIndex: 31, PictureType: "Back"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered pictures = %+v; want %+v", got, want)
	}
}

func TestEmbeddedArtworkStreamsAcceptsIndexBoundsAndStringScalars(t *testing.T) {
	data := []byte(`{"streams":[
		{"index":"4095","codec_name":"mjpeg","codec_type":"video","width":"32","height":24,"disposition":{"attached_pic":"1"}},
		{"index":0,"codec_name":"png","codec_type":"video","width":16,"height":"12","disposition":{"attached_pic":1}}
	]}` + " \r\n\t")
	catalog := map[int]Stream{
		0:    {Index: 0, Codec: "png", CodecType: "video", Width: 16, Height: 12, IsAttachedPicture: true},
		4095: {Index: 4095, Codec: "mjpeg", CodecType: "video", Width: 32, Height: 24, IsAttachedPicture: true},
	}
	got, err := embeddedArtworkStreams(data, catalog)
	if err != nil {
		t.Fatalf("parse bounded indexes and mixed scalar encodings: %v", err)
	}
	want := []EmbeddedPicture{{StreamIndex: 0, PictureType: "Other"}, {StreamIndex: 4095, PictureType: "Other"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pictures = %+v; want %+v", got, want)
	}
}

func TestEmbeddedArtworkStreamsWithoutAttachedCandidates(t *testing.T) {
	for name, data := range map[string]string{
		"empty stream list": `{"streams":[]}`,
		"ordinary streams": `{"streams":[
			{"index":0,"codec_type":"audio","codec_name":"aac"},
			{"index":1,"codec_type":"video","codec_name":"mjpeg","width":24,"height":18,"disposition":{"attached_pic":0},"tags":{"comment":"Cover (front)"}}
		]}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := embeddedArtworkStreams([]byte(data), nil)
			if err != nil || len(got) != 0 {
				t.Fatalf("metadata without attached pictures = %+v, %v; want empty success", got, err)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsRejectsMalformedOrMismatchedMetadata(t *testing.T) {
	const picture = `{"index":7,"codec_name":"png","codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1},"tags":{"comment":"Front"}}`
	const metadata = `{"streams":[` + picture + `]}`
	catalog := map[int]Stream{
		7: {Index: 7, Codec: "png", CodecType: "video", Width: 24, Height: 18, IsAttachedPicture: true},
	}
	for name, data := range map[string]string{
		"invalid JSON":                   `{"streams":[`,
		"trailing JSON object":           metadata + `{}`,
		"trailing JSON null":             metadata + `null`,
		"trailing garbage":               metadata + `garbage`,
		"missing expected stream":        `{"streams":[]}`,
		"missing streams":                `{}`,
		"null stream list":               `{"streams":null}`,
		"missing attachment disposition": strings.Replace(metadata, `,"disposition":{"attached_pic":1}`, "", 1),
		"attachment no longer attached":  strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":0`, 1),
		"unknown absolute index":         strings.Replace(metadata, `"index":7`, `"index":8`, 1),
		"codec differs from catalog":     strings.Replace(metadata, `"codec_name":"png"`, `"codec_name":"mjpeg"`, 1),
		"width differs from catalog":     strings.Replace(metadata, `"width":24`, `"width":25`, 1),
		"height differs from catalog":    strings.Replace(metadata, `"height":18`, `"height":19`, 1),
		"attached audio stream":          strings.Replace(metadata, `"codec_type":"video"`, `"codec_type":"audio"`, 1),
		"missing codec type":             strings.Replace(metadata, `,"codec_type":"video"`, "", 1),
		"duplicate attached index":       `{"streams":[` + picture + `,` + picture + `]}`,
		"duplicate index attached first": `{"streams":[` + picture + `,` + strings.Replace(picture, `"attached_pic":1`, `"attached_pic":0`, 1) + `]}`,
		"duplicate index attached last":  `{"streams":[` + strings.Replace(picture, `"attached_pic":1`, `"attached_pic":0`, 1) + `,` + picture + `]}`,
		"unexpected extra attachment":    `{"streams":[` + picture + `,` + strings.Replace(picture, `"index":7`, `"index":8`, 1) + `]}`,
		"fractional index":               strings.Replace(metadata, `"index":7`, `"index":7.5`, 1),
		"boolean index":                  strings.Replace(metadata, `"index":7`, `"index":true`, 1),
		"overflowing index":              strings.Replace(metadata, `"index":7`, `"index":9223372036854775808`, 1),
		"fractional width":               strings.Replace(metadata, `"width":24`, `"width":24.5`, 1),
		"boolean width":                  strings.Replace(metadata, `"width":24`, `"width":true`, 1),
		"missing width":                  strings.Replace(metadata, `,"width":24`, "", 1),
		"invalid height":                 strings.Replace(metadata, `"height":18`, `"height":"invalid"`, 1),
		"null height":                    strings.Replace(metadata, `"height":18`, `"height":null`, 1),
		"negative attachment flag":       strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":-1`, 1),
		"out of range attachment flag":   strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":2`, 1),
		"fractional attachment flag":     strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":0.5`, 1),
		"boolean attachment flag":        strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":true`, 1),
		"null attachment flag":           strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":null`, 1),
		"unknown attachment flag":        strings.Replace(metadata, `"attached_pic":1`, `"attached_pic":"N/A"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := embeddedArtworkStreams([]byte(data), catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) {
				t.Fatalf("malformed or mismatched metadata error = %v; want ErrEmbeddedArtwork", err)
			}
			if got != nil {
				t.Fatalf("rejected metadata exposed partial pictures: %+v", got)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsDoesNotOmitMissingCatalogCandidates(t *testing.T) {
	const first = `{"index":7,"codec_name":"png","codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1}}`
	const second = `{"index":23,"codec_name":"mjpeg","codec_type":"video","width":32,"height":20,"disposition":{"attached_pic":0}}`
	catalog := map[int]Stream{
		7:  {Index: 7, Codec: "png", CodecType: "video", Width: 24, Height: 18, IsAttachedPicture: true},
		23: {Index: 23, Codec: "mjpeg", CodecType: "video", Width: 32, Height: 20, IsAttachedPicture: true},
	}
	for name, data := range map[string]string{
		"candidate absent":       `{"streams":[` + first + `]}`,
		"candidate not attached": `{"streams":[` + first + `,` + second + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := embeddedArtworkStreams([]byte(data), catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) || got != nil {
				t.Fatalf("missing catalog candidate must reject all pictures: %+v, %v", got, err)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsRejectsMissingIndexWithoutUsingZero(t *testing.T) {
	const metadata = `{"streams":[{"index":0,"codec_name":"png","codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1}}]}`
	catalog := map[int]Stream{
		0: {Index: 0, Codec: "png", CodecType: "video", Width: 24, Height: 18, IsAttachedPicture: true},
	}
	for name, field := range map[string]string{
		"omitted": "",
		"null":    `"index":null,`,
		"empty":   `"index":"",`,
		"unknown": `"index":"N/A",`,
	} {
		t.Run(name, func(t *testing.T) {
			data := strings.Replace(metadata, `"index":0,`, field, 1)
			got, err := embeddedArtworkStreams([]byte(data), catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) || got != nil {
				t.Fatalf("missing index must not identify catalog stream zero: %+v, %v", got, err)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsRejectsOutOfRangeIndexesEvenWhenCatalogMatches(t *testing.T) {
	for _, index := range []int{-1, 4096} {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{"streams":[{"index":%d,"codec_name":"png","codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1}}]}`, index))
			catalog := map[int]Stream{
				index: {Index: index, Codec: "png", CodecType: "video", Width: 24, Height: 18, IsAttachedPicture: true},
			}
			got, err := embeddedArtworkStreams(data, catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) || got != nil {
				t.Fatalf("out of range index %d must fail even when the catalog agrees: %+v, %v", index, got, err)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsRejectsInvalidDimensionsEvenWhenCatalogMatches(t *testing.T) {
	const metadata = `{"streams":[{"index":7,"codec_name":"png","codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1}}]}`
	for _, test := range []struct {
		name   string
		data   string
		width  int
		height int
	}{
		{name: "missing width", data: strings.Replace(metadata, `,"width":24`, "", 1), height: 18},
		{name: "missing height", data: strings.Replace(metadata, `,"height":18`, "", 1), width: 24},
		{name: "both dimensions missing", data: strings.Replace(metadata, `,"width":24,"height":18`, "", 1)},
		{name: "null width", data: strings.Replace(metadata, `"width":24`, `"width":null`, 1), height: 18},
		{name: "unknown height", data: strings.Replace(metadata, `"height":18`, `"height":"N/A"`, 1), width: 24},
		{name: "zero width", data: strings.Replace(metadata, `"width":24`, `"width":0`, 1), height: 18},
		{name: "zero height", data: strings.Replace(metadata, `"height":18`, `"height":0`, 1), width: 24},
		{name: "negative width", data: strings.Replace(metadata, `"width":24`, `"width":-1`, 1), width: -1, height: 18},
		{name: "negative height", data: strings.Replace(metadata, `"height":18`, `"height":-1`, 1), width: 24, height: -1},
		{name: "width exceeds limit", data: strings.Replace(metadata, `"width":24`, `"width":16385`, 1), width: 16385, height: 18},
		{name: "height exceeds limit", data: strings.Replace(metadata, `"height":18`, `"height":16385`, 1), width: 24, height: 16385},
		{name: "pixel count exceeds limit", data: strings.Replace(metadata, `"width":24,"height":18`, `"width":6000,"height":5000`, 1), width: 6000, height: 5000},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := map[int]Stream{
				7: {Index: 7, Codec: "png", CodecType: "video", Width: test.width, Height: test.height, IsAttachedPicture: true},
			}
			got, err := embeddedArtworkStreams([]byte(test.data), catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) || got != nil {
				t.Fatalf("invalid picture dimensions must fail even when the catalog agrees: %+v, %v", got, err)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsAcceptsDimensionLimits(t *testing.T) {
	for _, test := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "maximum width", width: 16384, height: 1},
		{name: "maximum height", width: 1, height: 16384},
		{name: "maximum pixel count", width: 5120, height: 5120},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{"streams":[{"index":7,"codec_name":"png","codec_type":"video","width":%d,"height":%d,"disposition":{"attached_pic":1}}]}`, test.width, test.height))
			catalog := map[int]Stream{
				7: {Index: 7, Codec: "png", CodecType: "video", Width: test.width, Height: test.height, IsAttachedPicture: true},
			}
			got, err := embeddedArtworkStreams(data, catalog)
			if err != nil {
				t.Fatalf("picture dimensions at the supported limit must succeed: %v", err)
			}
			want := []EmbeddedPicture{{StreamIndex: 7, PictureType: "Other"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("pictures = %+v; want %+v", got, want)
			}
		})
	}
}

func TestEmbeddedArtworkStreamsRejectsUnsupportedCodecsEvenWhenCatalogMatches(t *testing.T) {
	for _, codec := range []string{"webp", "bmp", "h264", "jpeg", "PNG", ""} {
		t.Run(codec, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{"streams":[{"index":7,"codec_name":%q,"codec_type":"video","width":24,"height":18,"disposition":{"attached_pic":1}}]}`, codec))
			catalog := map[int]Stream{
				7: {Index: 7, Codec: codec, CodecType: "video", Width: 24, Height: 18, IsAttachedPicture: true},
			}
			got, err := embeddedArtworkStreams(data, catalog)
			if !errors.Is(err, ErrEmbeddedArtwork) || got != nil {
				t.Fatalf("unsupported codec %q must fail even when the catalog agrees: %+v, %v", codec, got, err)
			}
		})
	}
}
