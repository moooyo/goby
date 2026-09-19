package library

// ValidatePreferenceSort reuses the actual query consumer's sort contract.
// It is pure validation and does not open a catalog or grant a user identity.
func ValidatePreferenceSort(sortBy, sortOrder string) error {
	_, err := normalizeItemSort(Query{UserID: "preference-validation", SortBy: sortBy, SortOrder: sortOrder})
	return err
}
