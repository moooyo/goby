package library

// LibraryOptions contains only settings with a scanner consumer. Disabling an
// importer retains previously accepted source facts and administrator overrides.
type LibraryOptions struct {
	EnableLocalMetadata bool
	EnableLocalImages   bool
}

type LibraryOptionsUpdate struct {
	EnableLocalMetadata *bool
	EnableLocalImages   *bool
}

func DefaultLibraryOptions() LibraryOptions {
	return LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: true}
}

// A nil options pointer represents the historical scanner defaults. This also
// keeps in-memory callers from silently disabling both importers.
func EffectiveLibraryOptions(value Library) LibraryOptions {
	if value.Options == nil {
		return DefaultLibraryOptions()
	}
	return *value.Options
}

func applyLibraryOptions(previous LibraryOptions, update *LibraryOptionsUpdate) LibraryOptions {
	if update != nil {
		if update.EnableLocalMetadata != nil {
			previous.EnableLocalMetadata = *update.EnableLocalMetadata
		}
		if update.EnableLocalImages != nil {
			previous.EnableLocalImages = *update.EnableLocalImages
		}
	}
	return previous
}
