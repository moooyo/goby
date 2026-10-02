//go:build linux

package transcode

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func TestGeneratedWindowPackedPublicationKeepsEpochNumberAndExactOrigin(t *testing.T) {
	p := generatedWindowSamplePlan(2*ticksPerSecond+1, 6*ticksPerSecond)
	p.HLS.Window.StartNumber = 71
	directory := t.TempDir()
	publisher := &packedHLSPublisher{directory: directory, plan: p}
	var entries strings.Builder
	for _, number := range []int{71, 72} {
		name := fmt.Sprintf("segment-%06d.aac", number)
		packedTestWrite(t, directory, name+".tmp", packedTestAudio("aac"))
		fmt.Fprintf(&entries, "#EXTINF:1.000010,\n%s.tmp\n", name)
	}
	packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(entries.String())+"#EXT-X-ENDLIST\n"))
	if err := publisher.publish(true); err != nil {
		t.Fatal(err)
	}
	list, err := ParseMediaPlaylist(packedTestRead(t, directory, "main.m3u8"))
	if err != nil || list.Sequence != 71 || !list.Ended || len(list.Segments) != 2 ||
		!list.Segments[0].Discontinuity || list.Segments[1].Discontinuity {
		t.Fatalf("window namespace or encoder epoch was lost: %+v: %v", list, err)
	}
	origin := big.NewRat(88201*ticksPerSecond, 44100)
	for index, segment := range list.Segments {
		clock := new(big.Rat).Add(origin, new(big.Rat).SetInt64(int64(index)*10_000_100))
		clock.Mul(clock, big.NewRat(9, 1000))
		want := new(big.Int).Quo(clock.Num(), clock.Denom()).Uint64() & ((1 << 33) - 1)
		packedTestSegment(t, directory, segment.Name, packedTestAudio("aac"), want)
	}
	if err := publisher.publish(true); err != nil {
		t.Fatalf("repeat completion changed the measured epoch: %v", err)
	}
}

func TestGeneratedWindowPackedRejectsWrongPrivateNumberAndDiscontinuity(t *testing.T) {
	for name, entries := range map[string]string{
		"wrong number":    "#EXTINF:1.000000,\nsegment-000000.aac.tmp\n",
		"untrusted epoch": "#EXT-X-DISCONTINUITY\n#EXTINF:1.000000,\nsegment-000071.aac.tmp\n",
	} {
		t.Run(name, func(t *testing.T) {
			p := packedTestPlan("aac")
			p.HLS.Window = HLSWindow{EndTicks: 6 * ticksPerSecond, StartNumber: 71}
			directory := t.TempDir()
			publisher := &packedHLSPublisher{directory: directory, plan: p}
			packedTestWrite(t, directory, "segment-000000.aac.tmp", packedTestAudio("aac"))
			packedTestWrite(t, directory, "segment-000071.aac.tmp", packedTestAudio("aac"))
			packedTestWrite(t, directory, "segment-list.m3u8", []byte(packedTestPlaylist(entries)+"#EXT-X-ENDLIST\n"))
			if err := publisher.publish(true); err == nil {
				t.Fatal("untrusted private window was published")
			}
			packedTestAbsent(t, directory, "main.m3u8")
		})
	}
}

func TestGeneratedWindowPackedRationalClockWrapsAfterAccumulation(t *testing.T) {
	origin := new(big.Rat).Add(new(big.Rat).SetInt64(100_000*ticksPerSecond), big.NewRat(ticksPerSecond, 44100))
	for _, elapsed := range []int64{0, 10_000_100, 20_000_200} {
		clock := new(big.Rat).Add(origin, new(big.Rat).SetInt64(elapsed))
		tag := packedTimestampTagClock(clock)
		actual := binary.BigEndian.Uint64(tag[len(tag)-8:])
		expected := new(big.Rat).Mul(clock, big.NewRat(9, 1000))
		whole := new(big.Int).Quo(expected.Num(), expected.Denom())
		whole.Mod(whole, new(big.Int).Lsh(big.NewInt(1), 33))
		if actual != whole.Uint64() || actual>>33 != 0 {
			t.Fatalf("rational transport clock changed: %d != %s", actual, whole)
		}
	}
}
