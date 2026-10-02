//go:build !linux

package transcode

func openPooledFixedVolumeProvider(fixedVolumeConfig, fixedBackingPoolConfig) (fixedVolumeProvider, error) {
	return nil, errFixedVolumeUnavailable
}

func openPooledFixedVolumeProviderPrepared(fixedVolumeConfig, fixedBackingPoolConfig, func(*fixedPoolExecutablePreparation) error) (fixedVolumeProvider, error) {
	return nil, errFixedVolumeUnavailable
}
