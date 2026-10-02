package server

import (
	"fmt"

	"github.com/moooyo/goby/internal/transcode"
)

const hlsGeneratedWindowResourcesPerRendition = 2

type hlsGeneratedWindowExactResource struct {
	name     string
	identity string
}

// A table belongs to one already bound producer and one immutable slot plan.
// Initialization and media identities are independent even when the private
// initialization filename is reused by another producer. The table contains no
// reader ownership and does not establish source coverage or a native clock.
type hlsGeneratedWindowArtifactSet struct {
	producerID string
	plan       transcode.Plan
	resources  [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]hlsGeneratedWindowExactResource
}

func hlsGeneratedWindowResourceIndex(resource hlsGeneratedWindowResource) int {
	if resource.initialization {
		return 1
	}
	return 0
}

func hlsGeneratedWindowExactResourceName(plan transcode.Plan, resource hlsGeneratedWindowResource) string {
	if resource.number != plan.HLS.Window.StartNumber || hlsGeneratedWindowResourceName(plan, resource) == "" {
		return ""
	}
	prefix := ""
	if plan.HLS.RenditionCount > 0 {
		prefix = fmt.Sprintf("v%d-", resource.variant)
	}
	if resource.initialization {
		return prefix + "init.mp4"
	}
	extension := "ts"
	if plan.HLS.SegmentType == "fmp4" {
		extension = "m4s"
	}
	return fmt.Sprintf("%ssegment-%06d.%s", prefix, resource.number, extension)
}

// The caller supplies identities observed from held files and retains those
// files through independent media closure and the final identity fence. A
// complete private playlist proves the resource association only; constructing
// this table neither publishes a graph nor qualifies fMP4 for native playback.
func hlsGeneratedWindowArtifactSetFromPlaylists(producerID string, plan transcode.Plan, lists [transcode.MaxHLSRenditions]transcode.MediaPlaylist,
	identities [transcode.MaxHLSRenditions][hlsGeneratedWindowResourcesPerRendition]string) (hlsGeneratedWindowArtifactSet, error) {
	var set hlsGeneratedWindowArtifactSet
	invalid := func() (hlsGeneratedWindowArtifactSet, error) {
		return hlsGeneratedWindowArtifactSet{}, transcode.ErrInvalidTimeline
	}
	if producerID == "" || transcode.ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence {
		return invalid()
	}
	set.producerID = producerID
	set.plan = plan
	count := max(1, plan.HLS.RenditionCount)
	duration := plan.HLS.Window.EndTicks - plan.StartTicks
	for variant := 0; variant < transcode.MaxHLSRenditions; variant++ {
		list := lists[variant]
		if variant >= count {
			if list.Version != 0 || list.TargetDuration != 0 || list.Sequence != 0 || list.Type != "" || list.Independent ||
				list.Ended || list.InitName != "" || len(list.Segments) != 0 ||
				identities[variant] != ([hlsGeneratedWindowResourcesPerRendition]string{}) {
				return invalid()
			}
			continue
		}
		media := hlsGeneratedWindowResource{number: plan.HLS.Window.StartNumber, variant: variant}
		initialization := media
		initialization.initialization = true
		mediaName := hlsGeneratedWindowExactResourceName(plan, media)
		initName := hlsGeneratedWindowExactResourceName(plan, initialization)
		if mediaName == "" || duration <= 0 || !list.Ended || list.Type != "EVENT" ||
			list.Sequence != int64(media.number) || len(list.Segments) != 1 || list.Segments[0].Number != int64(media.number) ||
			list.Segments[0].Name != mediaName || !list.Segments[0].Discontinuity || list.Segments[0].DurationTicks != duration ||
			list.InitName != initName || identities[variant][0] == "" ||
			initName == "" && identities[variant][1] != "" || initName != "" && identities[variant][1] == "" {
			return invalid()
		}
		set.resources[variant][0] = hlsGeneratedWindowExactResource{name: mediaName, identity: identities[variant][0]}
		if initName != "" {
			set.resources[variant][1] = hlsGeneratedWindowExactResource{name: initName, identity: identities[variant][1]}
		}
	}
	if !set.distinctIdentities() {
		return invalid()
	}
	return set, nil
}

// Selection checks the whole table so a missing sibling or an unused entry
// cannot be filled by a scalar fallback. Exact filenames are interpreted only
// within the caller's separately proved producer ownership and slot closure.
func (set hlsGeneratedWindowArtifactSet) selectResource(producerID string, plan transcode.Plan, resource hlsGeneratedWindowResource) (hlsGeneratedWindowExactResource, bool) {
	if !set.valid(producerID, plan) || hlsGeneratedWindowExactResourceName(plan, resource) == "" {
		return hlsGeneratedWindowExactResource{}, false
	}
	return set.resources[resource.variant][hlsGeneratedWindowResourceIndex(resource)], true
}

func (set hlsGeneratedWindowArtifactSet) resourceForExactName(producerID string, plan transcode.Plan, name string) (hlsGeneratedWindowResource, bool) {
	if name == "" || !set.valid(producerID, plan) {
		return hlsGeneratedWindowResource{}, false
	}
	for variant := 0; variant < max(1, plan.HLS.RenditionCount); variant++ {
		for index, artifact := range set.resources[variant] {
			if artifact.name != "" && artifact.name == name {
				return hlsGeneratedWindowResource{number: plan.HLS.Window.StartNumber, variant: variant, initialization: index == 1}, true
			}
		}
	}
	return hlsGeneratedWindowResource{}, false
}

func (set hlsGeneratedWindowArtifactSet) valid(producerID string, plan transcode.Plan) bool {
	if producerID == "" || producerID != set.producerID || plan != set.plan || transcode.ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence {
		return false
	}
	count := max(1, plan.HLS.RenditionCount)
	for variant, artifacts := range set.resources {
		for index, artifact := range artifacts {
			if variant >= count {
				if artifact != (hlsGeneratedWindowExactResource{}) {
					return false
				}
				continue
			}
			resource := hlsGeneratedWindowResource{number: plan.HLS.Window.StartNumber, variant: variant, initialization: index == 1}
			name := hlsGeneratedWindowExactResourceName(plan, resource)
			if name == "" {
				if artifact != (hlsGeneratedWindowExactResource{}) {
					return false
				}
			} else if artifact.name != name || artifact.identity == "" {
				return false
			}
		}
	}
	return set.distinctIdentities()
}

// Filesystem identities describe held physical objects, not their content
// digests. Separate canonical resources in one producer cannot borrow a single
// object through another name or role. Equal initialization bytes in another
// producer remain a separate, independently measured content-hash contract.
func (set hlsGeneratedWindowArtifactSet) distinctIdentities() bool {
	var seen [transcode.MaxHLSRenditions * hlsGeneratedWindowResourcesPerRendition]string
	count := 0
	for _, artifacts := range set.resources {
		for _, artifact := range artifacts {
			if artifact.identity == "" {
				continue
			}
			for _, identity := range seen[:count] {
				if identity == artifact.identity {
					return false
				}
			}
			seen[count] = artifact.identity
			count++
		}
	}
	return true
}
