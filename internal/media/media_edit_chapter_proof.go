package media

// ChapLanguage is not part of ffprobe's chapter projection. Matroska defaults
// its absent value to eng, while FFmpeg's Matroska muxer emits und. Capturing
// the original container value is necessary to distinguish those semantics.
// Chapter UID is a container-local reference and is deliberately not retained.
type mediaEditChapterDisplayProof struct {
	StartNanoseconds uint64 `json:"start_ns"`
	EndNanoseconds   uint64 `json:"end_ns"`
	HasDisplay       bool   `json:"has_display"`
	Title            string `json:"title"`
	Language         string `json:"language"`
}

// This profile supports one legacy three-letter language per ChapterDisplay.
// BCP47 overrides, countries and multiple displays remain unadmitted rather
// than being collapsed into the demuxer's single title field.
func mediaEditChapterLanguage(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			return false
		}
	}
	return true
}
