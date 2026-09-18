package identity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxManagedPolicyBytes = 128 * 1024

// AccessSchedule grants access during a half-open interval in the server's
// configured local time. Overnight intervals must be split across two days.
type AccessSchedule struct {
	DayOfWeek string
	StartHour float64
	EndHour   float64
}

// ManagedPolicy contains supported configuration facts. Role and disabled
// state are separate account columns; unsupported upstream features are absent.
type ManagedPolicy struct {
	IsHidden                         bool
	IsHiddenRemotely                 bool
	IsHiddenFromUnusedDevices        bool
	MaxParentalRating                *int
	AllowTagOrRating                 bool
	BlockedTags                      []string
	IsTagBlockingModeInclusive       bool
	IncludeTags                      []string
	EnableUserPreferenceAccess       bool
	AccessSchedules                  []AccessSchedule
	BlockUnratedItems                []string
	EnableRemoteControlOfOtherUsers  bool
	EnableSharedDeviceControl        bool
	EnableRemoteAccess               bool
	EnableMediaPlayback              bool
	EnableAudioPlaybackTranscoding   bool
	EnableVideoPlaybackTranscoding   bool
	AutoRemoteQuality                int
	EnablePlaybackRemuxing           bool
	EnableContentDeletion            bool
	RestrictedFeatures               []string
	EnableContentDeletionFromFolders []string
	EnableContentDownloading         bool
	EnableSubtitleDownloading        bool
	EnableSubtitleManagement         bool
	EnabledFolders                   []string
	EnableAllFolders                 bool
	RemoteClientBitrateLimit         int
	ExcludedSubFolders               []string
	SimultaneousStreamLimit          int
	EnabledDevices                   []string
	EnableAllDevices                 bool
}

// DefaultManagedPolicy preserves existing access for accounts whose stored
// document predates a supported field. Destructive and cross-user powers remain
// opt-in; administrator status does not alter these editable facts.
func DefaultManagedPolicy() ManagedPolicy {
	return ManagedPolicy{
		EnableAllFolders: true, EnableAllDevices: true, EnableRemoteAccess: true,
		EnableMediaPlayback: true, EnablePlaybackRemuxing: true,
		EnableAudioPlaybackTranscoding: true, EnableVideoPlaybackTranscoding: true,
		EnableUserPreferenceAccess: true,
		EnableContentDownloading:   true, EnableSubtitleDownloading: true,
		EnabledFolders: []string{}, EnabledDevices: []string{}, ExcludedSubFolders: []string{},
		BlockedTags: []string{}, IncludeTags: []string{}, BlockUnratedItems: []string{},
		RestrictedFeatures: []string{}, EnableContentDeletionFromFolders: []string{},
		AccessSchedules: []AccessSchedule{},
	}
}

// ParseManagedPolicy validates every supported field before returning usable
// policy facts. Unknown canonical fields are retained by storage, but ignored
// here for forward compatibility. Wire adapters must restrict writable fields.
// Missing supported fields use the documented legacy defaults.
func ParseManagedPolicy(raw json.RawMessage) (ManagedPolicy, error) {
	return parseManagedPolicy(raw, false)
}

// ParseRuntimePolicy keeps independently decidable catalog and login access
// available when a stored playback permission has the wrong JSON type. Only
// the four playback booleans are projected to false; this never repairs the
// stored document, relaxes a write validator, or ignores malformed structure,
// duplicate/case-aliased keys, or another access restriction.
func ParseRuntimePolicy(raw json.RawMessage) (ManagedPolicy, error) {
	return parseManagedPolicy(raw, true)
}

func parseManagedPolicy(raw json.RawMessage, runtimePlayback bool) (ManagedPolicy, error) {
	if len(raw) == 0 || len(raw) > MaxManagedPolicyBytes || !utf8.Valid(raw) {
		return ManagedPolicy{}, managedUserFieldError("Policy", "policy must contain at most 128 KiB of UTF-8 JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodes := 0
	value, err := readPolicyValue(decoder, 0, &nodes)
	if err != nil {
		return ManagedPolicy{}, managedUserFieldError("Policy", "policy must contain bounded JSON with unique canonical keys")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ManagedPolicy{}, managedUserFieldError("Policy", "policy must contain one JSON object")
	}
	values, ok := value.(map[string]any)
	if !ok {
		return ManagedPolicy{}, managedUserFieldError("Policy", "policy must be a JSON object")
	}
	policy := DefaultManagedPolicy()
	typeOfPolicy := reflect.TypeOf(policy)
	known := make(map[string]string, typeOfPolicy.NumField())
	for index := 0; index < typeOfPolicy.NumField(); index++ {
		name := typeOfPolicy.Field(index).Name
		known[strings.ToLower(name)] = name
	}
	cleaned := make(map[string]any, len(known))
	for name, value := range values {
		if strings.EqualFold(name, "LockedOutDate") || strings.EqualFold(name, "InvalidLoginAttemptCount") {
			if name == "LockedOutDate" && value == nil {
				continue
			}
			number, validNumber := value.(json.Number)
			integer, numberErr := number.Int64()
			if (name != "LockedOutDate" && name != "InvalidLoginAttemptCount") || !validNumber || numberErr != nil || integer < 0 ||
				(name == "InvalidLoginAttemptCount" && integer > math.MaxInt32) {
				return ManagedPolicy{}, managedUserFieldError("Policy."+name, "server-managed login state must use its canonical nonnegative integer type")
			}
			continue
		}
		canonical, found := known[strings.ToLower(name)]
		if !found {
			continue
		}
		if runtimePlayback && name == canonical {
			switch canonical {
			case "EnableMediaPlayback", "EnablePlaybackRemuxing", "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding":
				if _, valid := value.(bool); !valid {
					value = false
				}
			}
		}
		if name != canonical || (value == nil && name != "MaxParentalRating") {
			return ManagedPolicy{}, managedUserFieldError("Policy."+canonical, "field must use its canonical name and declared JSON type")
		}
		cleaned[name] = value
	}
	if schedules, found := cleaned["AccessSchedules"]; found {
		list, ok := schedules.([]any)
		if !ok {
			return ManagedPolicy{}, managedUserFieldError("Policy.AccessSchedules", "access schedules must be an array")
		}
		for _, value := range list {
			schedule, ok := value.(map[string]any)
			if !ok || len(schedule) != 3 || schedule["DayOfWeek"] == nil || schedule["StartHour"] == nil || schedule["EndHour"] == nil {
				return ManagedPolicy{}, managedUserFieldError("Policy.AccessSchedules", "every schedule requires DayOfWeek, StartHour, and EndHour")
			}
		}
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil || json.Unmarshal(encoded, &policy) != nil {
		return ManagedPolicy{}, managedUserFieldError("Policy", "policy fields must use their declared JSON types")
	}
	fields := make(map[string]string)
	policy = canonicalManagedPolicy(policy, fields)
	if len(fields) != 0 {
		return ManagedPolicy{}, &ManagedUserValidationError{Fields: fields}
	}
	return policy, nil
}

// ParseStoredManagedPolicy retains strict stored-document validation. Runtime
// authorization uses ParseRuntimePolicy when an invalid playback boolean must
// not disable independently decidable access. Unknown fields grant no authority.
func ParseStoredManagedPolicy(raw json.RawMessage) (ManagedPolicy, error) {
	return ParseManagedPolicy(raw)
}

// ProjectManagedPolicy exposes editable facts with the runtime playback-only
// fallback. Other malformed fields deny the whole projection. Authorization
// callers must inspect ParseRuntimePolicy errors rather than treat a malformed
// document as an empty default; management writes still use ParseManagedPolicy.
func ProjectManagedPolicy(raw json.RawMessage) ManagedPolicy {
	policy, err := ParseRuntimePolicy(raw)
	if err == nil {
		return policy
	}
	return ManagedPolicy{IsHidden: true, IsHiddenRemotely: true, IsHiddenFromUnusedDevices: true,
		EnabledFolders: []string{}, EnabledDevices: []string{}, ExcludedSubFolders: []string{},
		BlockedTags: []string{}, IncludeTags: []string{}, BlockUnratedItems: []string{},
		RestrictedFeatures: []string{}, EnableContentDeletionFromFolders: []string{},
		AccessSchedules: []AccessSchedule{{DayOfWeek: "Everyday"}},
	}
}

func projectManagedPolicy(raw json.RawMessage) ManagedPolicy { return ProjectManagedPolicy(raw) }

func canonicalManagedPolicy(policy ManagedPolicy, fields map[string]string) ManagedPolicy {
	for _, list := range []struct {
		name  string
		value *[]string
	}{
		{"EnabledFolders", &policy.EnabledFolders}, {"EnabledDevices", &policy.EnabledDevices},
		{"ExcludedSubFolders", &policy.ExcludedSubFolders}, {"BlockedTags", &policy.BlockedTags},
		{"IncludeTags", &policy.IncludeTags}, {"BlockUnratedItems", &policy.BlockUnratedItems},
		{"RestrictedFeatures", &policy.RestrictedFeatures}, {"EnableContentDeletionFromFolders", &policy.EnableContentDeletionFromFolders},
	} {
		canonical, err := canonicalManagedFolders(*list.value)
		if err != nil {
			fields["Policy."+list.name] = "values must contain at most 256 unique nonempty identifiers of at most 256 UTF-8 bytes"
		} else {
			*list.value = canonical
		}
	}
	for _, category := range policy.BlockUnratedItems {
		if !slices.Contains([]string{"Movie", "Trailer", "Series", "Music", "Game", "Book", "LiveTvChannel", "LiveTvProgram", "ChannelContent", "Other"}, category) {
			fields["Policy.BlockUnratedItems"] = "unrated item categories must match the supported SDK enumeration"
		}
	}
	for name, value := range map[string]int{"AutoRemoteQuality": policy.AutoRemoteQuality,
		"RemoteClientBitrateLimit": policy.RemoteClientBitrateLimit, "SimultaneousStreamLimit": policy.SimultaneousStreamLimit} {
		if value < 0 || int64(value) > math.MaxInt32 {
			fields["Policy."+name] = "value must be a nonnegative 32-bit integer"
		}
	}
	if policy.MaxParentalRating != nil {
		value := *policy.MaxParentalRating
		if value < 0 || int64(value) > math.MaxInt32 {
			fields["Policy.MaxParentalRating"] = "parental rating must be null or a nonnegative 32-bit integer"
		}
		policy.MaxParentalRating = &value
	}
	if len(policy.AccessSchedules) > 256 {
		fields["Policy.AccessSchedules"] = "access schedules must contain at most 256 intervals"
	}
	schedules := make([]AccessSchedule, 0, len(policy.AccessSchedules))
	for _, schedule := range policy.AccessSchedules {
		if !validAccessSchedule(schedule) {
			fields["Policy.AccessSchedules"] = "schedules require a valid SDK day and 0 <= StartHour < EndHour <= 24"
		}
		schedules = append(schedules, schedule)
	}
	slices.SortFunc(schedules, func(left, right AccessSchedule) int {
		if compare := strings.Compare(left.DayOfWeek, right.DayOfWeek); compare != 0 {
			return compare
		}
		if left.StartHour < right.StartHour {
			return -1
		}
		if left.StartHour > right.StartHour {
			return 1
		}
		if left.EndHour < right.EndHour {
			return -1
		}
		if left.EndHour > right.EndHour {
			return 1
		}
		return 0
	})
	policy.AccessSchedules = slices.Compact(schedules)
	return policy
}

func validAccessSchedule(schedule AccessSchedule) bool {
	return slices.Contains([]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Everyday", "Weekday", "Weekend"}, schedule.DayOfWeek) &&
		!math.IsNaN(schedule.StartHour) && !math.IsNaN(schedule.EndHour) && !math.IsInf(schedule.StartHour, 0) && !math.IsInf(schedule.EndHour, 0) &&
		schedule.StartHour >= 0 && schedule.StartHour < schedule.EndHour && schedule.EndHour <= 24
}

// AllowsDevice evaluates the reported device identifier from the credential.
func (policy ManagedPolicy) AllowsDevice(deviceID string) bool {
	return policy.EnableAllDevices || (deviceID != "" && slices.Contains(policy.EnabledDevices, deviceID))
}

// CanControlClientSession applies current account policy to a safe session
// projection. The caller must separately authenticate both endpoints and check
// target command capabilities. A userless server credential is handled by its
// existing application-principal authority rather than a fabricated User.
func CanControlClientSession(controller User, target ClientSession) bool {
	if controller.ID == "" || controller.IsDisabled {
		return false
	}
	policy, err := ParseRuntimePolicy(controller.Policy)
	if err != nil || !policy.AllowsFeature(FeatureRemoteControl) {
		return false
	}
	if controller.IsAdministrator {
		return true
	}
	if target.Kind == "emby" && target.UserID != "" {
		return target.UserID == controller.ID || policy.EnableRemoteControlOfOtherUsers
	}
	return target.Kind == ApplicationKeyKind && target.UserID == "" && policy.EnableSharedDeviceControl
}

// AllowsAccessAt evaluates the union of explicit access intervals. An empty
// schedule imposes no time restriction. EndHour is exclusive.
func (policy ManagedPolicy) AllowsAccessAt(now time.Time) bool {
	if len(policy.AccessSchedules) == 0 {
		return true
	}
	now = now.Local()
	day := now.Weekday()
	hour := float64(now.Hour()) + float64(now.Minute())/60 + float64(now.Second())/3600 + float64(now.Nanosecond())/3.6e12
	for _, schedule := range policy.AccessSchedules {
		if !validAccessSchedule(schedule) {
			continue
		}
		matches := schedule.DayOfWeek == day.String() || schedule.DayOfWeek == "Everyday" ||
			(schedule.DayOfWeek == "Weekday" && day >= time.Monday && day <= time.Friday) ||
			(schedule.DayOfWeek == "Weekend" && (day == time.Saturday || day == time.Sunday))
		if matches && hour >= schedule.StartHour && hour < schedule.EndHour {
			return true
		}
	}
	return false
}

// IsLocalPeer classifies only the trusted transport peer, never raw forwarded
// headers or a client-reported device address. An absent address is not local.
func IsLocalPeer(peerIP string) bool {
	address, err := netip.ParseAddr(peerIP)
	if err != nil {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast()
}

func loginPolicyAllows(raw json.RawMessage, deviceID string, now time.Time) bool {
	policy, err := ParseRuntimePolicy(raw)
	if err != nil || !policy.AllowsDevice(deviceID) || !policy.AllowsAccessAt(now) {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	if value, found := fields["LockedOutDate"]; found && string(value) != "null" {
		var lockedOut int64
		if json.Unmarshal(value, &lockedOut) != nil || lockedOut != 0 {
			return false
		}
	}
	return true
}

func readPolicyValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes = *nodes + 1
	if depth > 8 || *nodes > 8192 {
		return nil, ErrInvalidInput
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := token.(string)
				if !ok || !validRevalidationID(key) || len(object) >= 64 || seen[strings.ToLower(key)] {
					return nil, ErrInvalidInput
				}
				seen[strings.ToLower(key)] = true
				child, err := readPolicyValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, ErrInvalidInput
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				if len(array) >= 256 {
					return nil, ErrInvalidInput
				}
				child, err := readPolicyValue(decoder, depth+1, nodes)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, ErrInvalidInput
			}
			return array, nil
		}
	case string:
		if len(value) > 16384 {
			return nil, ErrInvalidInput
		}
		return value, nil
	case bool, json.Number, nil:
		return value, nil
	}
	return nil, fmt.Errorf("%w: unexpected JSON delimiter", ErrInvalidInput)
}
