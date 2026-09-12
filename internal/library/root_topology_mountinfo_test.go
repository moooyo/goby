package library

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseRootMountInfoDecodesEscapesOnceAndRetainsOptions(t *testing.T) {
	raw := []byte(`42 7 4294967295:0 /root\040name\011tab\012line\134tail /media\040disk\134040 rw,nosuid,nodev shared:12 master:9 propagate_from:8 future:opaque:value - futurefs source\040name\011tab\012line\134end rw,option=one,option=two` + "\n")
	entries, err := parseRootMountInfo(raw)
	if err != nil || len(entries) != 1 {
		t.Fatalf("parse complete mountinfo record: %v", err)
	}
	want := rootMountInfo{ID: 42, ParentID: 7, Major: ^uint32(0), Minor: 0,
		Root: "/root name\ttab\nline\\tail", MountPoint: "/media disk\\040", FilesystemType: "futurefs", Source: "source name\ttab\nline\\end",
		Options: []string{"rw", "nosuid", "nodev"}, OptionalFields: []string{"shared:12", "master:9", "propagate_from:8", "future:opaque:value"},
		SuperOptions: []string{"rw", "option=one", "option=two"}}
	if !reflect.DeepEqual(entries[0], want) {
		t.Fatalf("decoded mountinfo = %+v, want %+v", entries[0], want)
	}
	for index := range raw {
		raw[index] = 'x'
	}
	if !reflect.DeepEqual(entries[0], want) {
		t.Fatal("parsed records retained mutable caller bytes")
	}
	bare, err := parseRootMountInfo([]byte("2147483647 2147483647 0:4294967295 / / rw unbindable - unknownfs none rw\n"))
	if err != nil || len(bare) != 1 || bare[0].ID != 2147483647 || bare[0].Minor != ^uint32(0) || !slices.Equal(bare[0].OptionalFields, []string{"unbindable"}) {
		t.Fatalf("canonical numeric boundaries or unknown filesystem were rejected: %v", err)
	}
}

func TestParseRootMountInfoAcceptsObservedExternalNamespaceBindMounts(t *testing.T) {
	// These are the two legitimate nsfs rows that stopped source39's real
	// filesystem tests. Namespace roots are opaque dentries, not relative paths.
	raw := "273 28 0:4 net:[4026532415] /run/docker/netns/22234248d7ea rw shared:181 - nsfs nsfs rw\n" +
		"272 28 0:4 net:[4026532480] /run/docker/netns/d862da0f77f4 rw shared:186 - nsfs nsfs rw\n"
	namespaces, err := parseRootMountInfo([]byte(raw))
	if err != nil || len(namespaces) != 2 || namespaces[0].Root != "net:[4026532415]" || namespaces[1].Root != "net:[4026532480]" {
		t.Fatalf("the observed namespace records were not preserved: %v", err)
	}
	entries, mapping := rootMountPlanFixture()
	want, err := planRootMounts(entries, mapping, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	got, err := planRootMounts(append(entries, namespaces...), mapping, 10, 20)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("external namespace mounts affected the directory topology: %v", err)
	}
}

func TestParseRootMountInfoNamespaceRootsRemainStrictlyTyped(t *testing.T) {
	for _, namespace := range []string{"mnt", "net", "uts", "ipc", "pid", "user", "cgroup", "time"} {
		raw := "10 1 0:4 " + namespace + ":[18446744073709551615] /run/namespace rw - nsfs nsfs rw\n"
		if entries, err := parseRootMountInfo([]byte(raw)); err != nil || len(entries) != 1 {
			t.Fatalf("canonical namespace root %s was rejected: %v", namespace, err)
		}
	}
	for _, root := range []string{"relative", "net", "net:[]", "net:[0]", "net:[01]", "net:[+1]", "net:[-1]",
		"net:[18446744073709551616]", "net:[1]suffix", "net:[1]/child", "Net:[1]", "unknown:[1]", "net:[[1]]"} {
		t.Run(root, func(t *testing.T) {
			raw := "10 1 0:4 " + root + " /run/namespace rw - nsfs nsfs rw\n"
			if entries, err := parseRootMountInfo([]byte(raw)); !errors.Is(err, ErrInvalidRootTopology) || entries != nil {
				t.Fatalf("a malformed namespace root returned a table: %v", err)
			}
		})
	}
	for _, raw := range []string{
		"10 1 0:4 net:[1] /run/namespace rw - ext4 none rw\n",
		"10 1 0:4 net:[1] relative rw - nsfs nsfs rw\n",
		"10 1 0:4 net:[1] /run/../namespace rw - nsfs nsfs rw\n",
	} {
		if entries, err := parseRootMountInfo([]byte(raw)); !errors.Is(err, ErrInvalidRootTopology) || entries != nil {
			t.Fatalf("namespace syntax widened another filesystem or mountpoint rule: %v", err)
		}
	}
}

func TestPlanRootMountsRejectsNamespaceDentriesWithinEveryRelevantRole(t *testing.T) {
	for _, id := range []int{10, 15, 20, 31} {
		t.Run(fmt.Sprintf("record_%d", id), func(t *testing.T) {
			entries, mapping := rootMountPlanFixture()
			for index := range entries {
				if entries[index].ID == id {
					entries[index].Root = "net:[4026532415]"
					entries[index].FilesystemType, entries[index].Source = "nsfs", "nsfs"
				}
			}
			if plan, err := planRootMounts(entries, mapping, 10, 20); !errors.Is(err, ErrRootTopologyAmbiguous) ||
				len(plan.Records) != 0 || len(plan.Nested) != 0 {
				t.Fatalf("a namespace dentry became a complete directory plan: %v", err)
			}
		})
	}
}

func TestParseRootMountInfoRejectsMalformedOrIncompleteRecords(t *testing.T) {
	base := "10 1 8:1 / /media rw - ext4 /dev/test rw\n"
	for _, test := range []struct{ name, raw string }{
		{"empty", ""}, {"truncated last line", strings.TrimSuffix(base, "\n")}, {"blank line", base + "\n"},
		{"CRLF", strings.TrimSuffix(base, "\n") + "\r\n"}, {"leading space", " " + base},
		{"trailing space", strings.TrimSuffix(base, "\n") + " \n"}, {"duplicate delimiter", strings.Replace(base, "10 1", "10  1", 1)},
		{"raw tab", strings.Replace(base, "/media", "/media\ttab", 1)}, {"NUL", strings.Replace(base, "/media", "/media\x00", 1)},
		{"zero ID", strings.Replace(base, "10 1", "0 1", 1)}, {"negative ID", strings.Replace(base, "10 1", "-1 1", 1)},
		{"signed ID", strings.Replace(base, "10 1", "+10 1", 1)}, {"padded ID", strings.Replace(base, "10 1", "010 1", 1)},
		{"legacy ID overflow", strings.Replace(base, "10 1", "2147483648 1", 1)}, {"zero parent", strings.Replace(base, "10 1", "10 0", 1)},
		{"padded parent", strings.Replace(base, "10 1", "10 01", 1)}, {"padded major", strings.Replace(base, "8:1", "08:1", 1)},
		{"negative major", strings.Replace(base, "8:1", "-8:1", 1)}, {"major overflow", strings.Replace(base, "8:1", "4294967296:1", 1)},
		{"missing minor", strings.Replace(base, "8:1", "8:", 1)}, {"minor overflow", strings.Replace(base, "8:1", "8:4294967296", 1)},
		{"extra device separator", strings.Replace(base, "8:1", "8:1:2", 1)}, {"missing separator", strings.Replace(base, " - ", " ", 1)},
		{"extra post-separator field", strings.TrimSuffix(base, "\n") + " extra\n"}, {"missing super options", strings.TrimSuffix(base, " rw\n") + "\n"},
		{"empty mount option", strings.Replace(base, " rw -", " rw,,nosuid -", 1)}, {"empty super option", strings.TrimSuffix(base, "\n") + ",\n"},
		{"relative root", strings.Replace(base, " / /media", " relative /media", 1)}, {"relative mountpoint", strings.Replace(base, "/media", "media", 1)},
		{"noncanonical root", strings.Replace(base, " / /media", " /a/../b /media", 1)}, {"noncanonical mountpoint", strings.Replace(base, "/media", "/media/", 1)},
		{"unsupported escape", strings.Replace(base, "/media", `/media\041`, 1)}, {"short escape", strings.Replace(base, "/media", `/media\04`, 1)},
		{"bare backslash", strings.Replace(base, "/dev/test", `/dev/test\`, 1)}, {"invalid UTF-8", strings.Replace(base, "/media", "/media\xff", 1)},
		{"shared without value", strings.Replace(base, " - ", " shared - ", 1)}, {"zero master", strings.Replace(base, " - ", " master:0 - ", 1)},
		{"padded propagation group", strings.Replace(base, " - ", " propagate_from:01 - ", 1)},
		{"duplicate known optional field", strings.Replace(base, " - ", " shared:2 shared:3 - ", 1)},
		{"unbindable argument", strings.Replace(base, " - ", " unbindable:1 - ", 1)},
		{"empty optional tag", strings.Replace(base, " - ", " :value - ", 1)}, {"empty future argument", strings.Replace(base, " - ", " future: - ", 1)},
		{"duplicate ID", base + base},
	} {
		t.Run(test.name, func(t *testing.T) {
			if entries, err := parseRootMountInfo([]byte(test.raw)); !errors.Is(err, ErrInvalidRootTopology) || entries != nil {
				t.Fatalf("invalid mountinfo returned a partial table or wrong error: %v", err)
			}
		})
	}
}

func TestParseRootMountInfoEnforcesByteLineEntryAndDecodedPathBounds(t *testing.T) {
	if entries, err := parseRootMountInfo(bytes.Repeat([]byte{'x'}, MaxRootMountInfoBytes+1)); !errors.Is(err, ErrRootTopologyLimit) || entries != nil {
		t.Fatalf("oversized mountinfo = %v", err)
	}
	prefix := "10 1 0:0 / /media rw - ext4 none rw,"
	line := prefix + strings.Repeat("x", MaxRootMountInfoLineBytes-len(prefix))
	if entries, err := parseRootMountInfo([]byte(line + "\n")); err != nil || len(entries) != 1 {
		t.Fatalf("exact line budget was rejected: %v", err)
	}
	if entries, err := parseRootMountInfo([]byte(line + "x\n")); !errors.Is(err, ErrRootTopologyLimit) || entries != nil {
		t.Fatalf("excess line budget = %v", err)
	}
	path := "/" + strings.Repeat("p", maxRootTopologyPathBytes-1)
	if entries, err := parseRootMountInfo([]byte("10 1 0:0 / " + path + " rw - ext4 none rw\n")); err != nil || len(entries) != 1 || entries[0].MountPoint != path {
		t.Fatalf("exact decoded path budget was rejected: %v", err)
	}
	if entries, err := parseRootMountInfo([]byte("10 1 0:0 / " + path + "p rw - ext4 none rw\n")); !errors.Is(err, ErrRootTopologyLimit) || entries != nil {
		t.Fatalf("excess decoded path budget = %v", err)
	}
	var table strings.Builder
	for index := 1; index <= MaxRootMountInfoEntries; index++ {
		fmt.Fprintf(&table, "%d 1 0:0 / /media rw - ext4 none rw\n", index)
	}
	if entries, err := parseRootMountInfo([]byte(table.String())); err != nil || len(entries) != MaxRootMountInfoEntries {
		t.Fatalf("exact mount record limit was rejected: %v", err)
	}
	fmt.Fprintf(&table, "%d 1 0:0 / /media rw - ext4 none rw\n", MaxRootMountInfoEntries+1)
	if entries, err := parseRootMountInfo([]byte(table.String())); !errors.Is(err, ErrRootTopologyLimit) || entries != nil {
		t.Fatalf("excess mount record budget = %v", err)
	}
}

func TestPlanRootMountsUsesDescriptorIDsAndKeepsOnlyDeterministicRelevantRecords(t *testing.T) {
	entries, mapping := rootMountPlanFixture()
	plan, err := planRootMounts(entries, mapping, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Anchor.ID != 10 || plan.Root.ID != 20 || !slices.Equal(rootMountTestIDs(plan.Nested), []int{31, 32, 30}) ||
		!slices.Equal(rootMountTestIDs(plan.Records), []int{10, 15, 20, 30, 31, 32}) {
		t.Fatalf("mount plan selected the wrong scope: %+v", plan)
	}
	slices.Reverse(entries)
	reordered, err := planRootMounts(entries, mapping, 10, 20)
	if err != nil || !reflect.DeepEqual(plan, reordered) {
		t.Fatalf("input enumeration order changed the scoped plan: %v", err)
	}
	for index := range entries {
		if entries[index].ID == 40 {
			entries[index].FilesystemType = "another-unknown-filesystem"
			entries[index].OptionalFields = []string{"new_external_tag:opaque"}
		}
	}
	entries = append(entries, rootMountInfoFixture(42, 41, "/outside"))
	externalChange, err := planRootMounts(entries, mapping, 10, 20)
	if err != nil || !reflect.DeepEqual(plan, externalChange) {
		t.Fatalf("unrelated mount changes entered the scoped comparison: %v", err)
	}
	for index := range entries {
		if entries[index].ID == 20 {
			entries[index].Options[0] = "ro"
		}
	}
	changed, err := planRootMounts(entries, mapping, 10, 20)
	if err != nil || reflect.DeepEqual(plan.Records, changed.Records) || plan.Root.Options[0] != "rw" {
		t.Fatalf("relevant options were lost or caller mutation changed the previous plan: %v", err)
	}
	changed.Root.Options[0] = "caller-change"
	for _, record := range changed.Records {
		if record.ID == changed.Root.ID && record.Options[0] != "ro" {
			t.Fatal("returned plan records share mutable option slices")
		}
	}
}

func TestPlanRootMountsExcludesStackedAncestorsBeforeTheApprovedPath(t *testing.T) {
	mapping := RootTopologyMapping{ApprovedPath: "/media", RegisteredPath: "/media/library"}
	entries := []rootMountInfo{
		rootMountInfoFixture(1, 1, "/"), rootMountInfoFixture(5, 1, "/"),
		rootMountInfoFixture(6, 5, "/media/library/nested"),
	}
	entries[0].OptionalFields = []string{"external_future_tag"}
	plan, err := planRootMounts(entries, mapping, 5, 5)
	if err != nil || plan.Anchor.ID != 5 || plan.Root.ID != 5 || !slices.Equal(rootMountTestIDs(plan.Nested), []int{6}) || !slices.Equal(rootMountTestIDs(plan.Records), []int{5, 6}) {
		t.Fatalf("a preceding external stack affected the actual descriptor scope: %v", err)
	}
	entries[2].ParentID = 1
	if _, err := planRootMounts(entries, mapping, 5, 5); !errors.Is(err, ErrRootTopologyAmbiguous) {
		t.Fatalf("a hidden child below the preceding stack was accepted: %v", err)
	}
}

func TestPlanRootMountsRejectsRelatedUnknownOptionalFields(t *testing.T) {
	for _, id := range []int{10, 15, 20, 31} {
		t.Run(fmt.Sprintf("record_%d", id), func(t *testing.T) {
			entries, mapping := rootMountPlanFixture()
			for index := range entries {
				if entries[index].ID == id {
					entries[index].OptionalFields = []string{"future_topology:uninterpreted"}
				}
			}
			if plan, err := planRootMounts(entries, mapping, 10, 20); !errors.Is(err, ErrRootTopologyAmbiguous) || len(plan.Records) != 0 {
				t.Fatalf("related unknown topology semantics produced a partial plan: %v", err)
			}
		})
	}
}

func TestPlanRootMountsRejectsMissingDescriptorsInvalidMappingsAndBrokenChains(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]rootMountInfo, *RootTopologyMapping, *int, *int) []rootMountInfo
		want error
	}{
		{"missing anchor", func(entries []rootMountInfo, _ *RootTopologyMapping, anchor, _ *int) []rootMountInfo {
			*anchor = 999
			return entries
		}, ErrRootTopologyChanged},
		{"missing root", func(entries []rootMountInfo, _ *RootTopologyMapping, _, root *int) []rootMountInfo {
			*root = 999
			return entries
		}, ErrRootTopologyChanged},
		{"noncanonical mapping", func(entries []rootMountInfo, mapping *RootTopologyMapping, _, _ *int) []rootMountInfo {
			mapping.ApprovedPath = "/media/"
			return entries
		}, ErrInvalidRootTopology},
		{"path prefix escape", func(entries []rootMountInfo, mapping *RootTopologyMapping, _, _ *int) []rootMountInfo {
			mapping.RegisteredPath = "/media-other/library"
			return entries
		}, ErrInvalidRootTopology},
		{"duplicate ID", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			return append(entries, entries[0])
		}, ErrInvalidRootTopology},
		{"missing intermediate parent", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			return slices.DeleteFunc(entries, func(entry rootMountInfo) bool { return entry.ID == 15 })
		}, ErrRootTopologyChanged},
		{"parent mountpoint does not contain child", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			for index := range entries {
				if entries[index].ID == 15 {
					entries[index].MountPoint = "/other"
				}
			}
			return entries
		}, ErrRootTopologyChanged},
		{"descriptor mountpoint does not contain registered path", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			for index := range entries {
				if entries[index].ID == 20 {
					entries[index].MountPoint += "-other"
				}
			}
			return entries
		}, ErrRootTopologyChanged},
		{"nested parent cycle", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			for index := range entries {
				if entries[index].ID == 31 {
					entries[index].ParentID = 31
				}
			}
			return entries
		}, ErrRootTopologyAmbiguous},
		{"root does not reach anchor", func(entries []rootMountInfo, _ *RootTopologyMapping, _, _ *int) []rootMountInfo {
			for index := range entries {
				if entries[index].ID == 20 {
					entries[index].ParentID = 1
				}
			}
			return entries
		}, ErrRootTopologyAmbiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, mapping := rootMountPlanFixture()
			anchorID, rootID := 10, 20
			entries = test.edit(entries, &mapping, &anchorID, &rootID)
			if plan, err := planRootMounts(entries, mapping, anchorID, rootID); !errors.Is(err, test.want) || len(plan.Records) != 0 {
				t.Fatalf("unsafe scoped topology = %v, want %v", err, test.want)
			}
		})
	}
}

func TestPlanRootMountsRejectsStackedAndAncestorHiddenMounts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func([]rootMountInfo, RootTopologyMapping) []rootMountInfo
	}{
		{"same-path siblings", func(entries []rootMountInfo, mapping RootTopologyMapping) []rootMountInfo {
			return append(entries, rootMountInfoFixture(35, 20, mapping.RegisteredPath+"/a"))
		}},
		{"same-path stacked child", func(entries []rootMountInfo, mapping RootTopologyMapping) []rootMountInfo {
			return append(entries, rootMountInfoFixture(35, 31, mapping.RegisteredPath+"/a"))
		}},
		{"stack at registered root", func(entries []rootMountInfo, mapping RootTopologyMapping) []rootMountInfo {
			return append(entries, rootMountInfoFixture(35, 20, mapping.RegisteredPath))
		}},
		{"hidden mount in anchor-to-root corridor", func(entries []rootMountInfo, _ RootTopologyMapping) []rootMountInfo {
			return append(entries, rootMountInfoFixture(35, 10, "/media/project"))
		}},
		{"ancestor overlay hides old child", func(entries []rootMountInfo, mapping RootTopologyMapping) []rootMountInfo {
			for index := range entries {
				if entries[index].ID == 32 {
					entries[index].ParentID = 20
				}
			}
			return append(entries, rootMountInfoFixture(35, 20, mapping.RegisteredPath+"/a-other"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, mapping := rootMountPlanFixture()
			entries = test.edit(entries, mapping)
			for attempt := 0; attempt < 2; attempt++ {
				if plan, err := planRootMounts(entries, mapping, 10, 20); !errors.Is(err, ErrRootTopologyAmbiguous) || len(plan.Records) != 0 {
					t.Fatalf("hidden mount topology produced a partial plan: %v", err)
				}
				slices.Reverse(entries)
			}
		})
	}
}

func TestPlanRootMountsCannotAdoptCorridorMountThroughANestedParentChain(t *testing.T) {
	mapping := RootTopologyMapping{ApprovedPath: "/media", RegisteredPath: "/media/library"}
	entries := []rootMountInfo{
		rootMountInfoFixture(3, 2, "/media/library/nested"),
		rootMountInfoFixture(2, 1, "/media/library"),
		rootMountInfoFixture(1, 99, "/media"),
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := planRootMounts(entries, mapping, 1, 1); !errors.Is(err, ErrRootTopologyAmbiguous) {
			t.Fatalf("an inconsistent root descriptor adopted a covering mount: %v", err)
		}
		slices.Reverse(entries)
	}
}

func TestPlanRootMountsBoundsNestedMountsAndDirectRecordInputs(t *testing.T) {
	mapping := RootTopologyMapping{ApprovedPath: "/media", RegisteredPath: "/media"}
	entries := []rootMountInfo{rootMountInfoFixture(1, 99, "/media")}
	for index := 0; index < MaxRootTopologyBoundaries; index++ {
		entries = append(entries, rootMountInfoFixture(index+2, 1, fmt.Sprintf("/media/nested-%04d", index)))
	}
	if plan, err := planRootMounts(entries, mapping, 1, 1); err != nil || len(plan.Nested) != MaxRootTopologyBoundaries || len(plan.Records) != MaxRootTopologyBoundaries+1 {
		t.Fatalf("exact nested boundary budget was rejected: %v", err)
	}
	entries = append(entries, rootMountInfoFixture(MaxRootTopologyBoundaries+2, 1, "/media/one-more"))
	if _, err := planRootMounts(entries, mapping, 1, 1); !errors.Is(err, ErrRootTopologyLimit) {
		t.Fatalf("excess nested boundary budget = %v", err)
	}
	if _, err := planRootMounts(make([]rootMountInfo, MaxRootMountInfoEntries+1), mapping, 1, 1); !errors.Is(err, ErrRootTopologyLimit) {
		t.Fatalf("excess direct record count = %v", err)
	}
	oversized := rootMountInfoFixture(1, 99, "/media")
	oversized.SuperOptions = []string{strings.Repeat("x", MaxRootMountInfoLineBytes)}
	if _, err := planRootMounts([]rootMountInfo{oversized}, mapping, 1, 1); !errors.Is(err, ErrRootTopologyLimit) {
		t.Fatalf("unbounded direct record options = %v", err)
	}
	oversized = rootMountInfoFixture(1, 99, "/media")
	oversized.Options = nil
	if _, err := planRootMounts([]rootMountInfo{oversized}, mapping, 1, 1); !errors.Is(err, ErrInvalidRootTopology) {
		t.Fatalf("incomplete direct record options = %v", err)
	}
}

func rootMountInfoFixture(id, parent int, point string) rootMountInfo {
	return rootMountInfo{ID: id, ParentID: parent, Major: 8, Minor: 1, Root: "/", MountPoint: point,
		FilesystemType: "ext4", Source: "/dev/test", Options: []string{"rw"}, OptionalFields: []string{}, SuperOptions: []string{"rw"}}
}

func rootMountPlanFixture() ([]rootMountInfo, RootTopologyMapping) {
	mapping := RootTopologyMapping{ApprovedPath: "/media", RegisteredPath: "/media/project/library"}
	entries := []rootMountInfo{
		rootMountInfoFixture(32, 31, mapping.RegisteredPath+"/a/deeper"),
		rootMountInfoFixture(41, 40, "/outside"),
		rootMountInfoFixture(20, 15, mapping.RegisteredPath),
		rootMountInfoFixture(1, 1, "/"),
		rootMountInfoFixture(15, 10, "/media/project"),
		rootMountInfoFixture(30, 20, mapping.RegisteredPath+"/z"),
		rootMountInfoFixture(10, 1, "/media"),
		rootMountInfoFixture(31, 20, mapping.RegisteredPath+"/a"),
		rootMountInfoFixture(40, 1, "/outside"),
		rootMountInfoFixture(50, 15, mapping.RegisteredPath+"-other"),
	}
	entries[8].FilesystemType = "unknown-filesystem"
	entries[8].OptionalFields = []string{"unknown_external:opaque"}
	return entries, mapping
}

func rootMountTestIDs(entries []rootMountInfo) []int {
	ids := make([]int, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ID
	}
	return ids
}
