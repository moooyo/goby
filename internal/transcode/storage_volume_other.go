//go:build !linux

package transcode

func openFixedVolumeProvider(fixedVolumeConfig) (fixedVolumeProvider, error) {
	return nil, errFixedVolumeUnavailable
}
