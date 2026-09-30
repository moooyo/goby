package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const syntheticClipFrames = 80

func syntheticRandom(seed *uint32) byte {
	*seed ^= *seed << 13
	*seed ^= *seed >> 17
	*seed ^= *seed << 5
	return byte(*seed >> 24)
}

// This constructs direct-memory test inputs. It does not claim extraction,
// source-container PTS auditing, or real media/source independence evidence.
func makeSyntheticSources(kind string) map[string]source {
	canvas := make([]byte, 256*256)
	seed := uint32(0x51f17e01)
	for i := range canvas {
		canvas[i] = syntheticRandom(&seed)
	}
	ids := []string{"SYN-A", "SYN-B", "SYN-C"}
	starts := []int{200, 270, 340}
	result := map[string]source{}
	for index, id := range ids {
		raw := make([]byte, 1200*frameBytes)
		privateSeed := uint32(0xa5510001) + uint32(index)*0x10101
		for i := range raw {
			raw[i] = syntheticRandom(&privateSeed)
		}
		if kind != "different-content" {
			for clip := 0; clip < syntheticClipFrames; clip++ {
				x, y := clip*2, clip
				if kind == "shared-static" {
					x, y = 0, 0
				}
				for row := 0; row < 96; row++ {
					copy(raw[(starts[index]+clip)*frameBytes+row*96:(starts[index]+clip)*frameBytes+(row+1)*96], canvas[(y+row)*256+x:(y+row)*256+x+96])
				}
			}
		}
		pts := make([]float64, 1200)
		for i := range pts {
			pts[i] = float64(i) / 10
		}
		hash := sha256.Sum256(raw)
		digest := hex.EncodeToString(hash[:])
		ptsData, _ := json.Marshal(pts)
		ptsHash := sha256.Sum256(ptsData)
		result[id] = source{Info: inputSource{ID: id, SourceSHA256: digest}, PTS: pts, Raw: raw, GraySHA256: digest, PTSSHA256: hex.EncodeToString(ptsHash[:])}
	}
	return result
}
