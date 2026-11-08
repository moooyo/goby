package transcode

import (
	"strconv"
	"strings"
)

// appendDownMixAudioFilter retains the existing sample-format negotiation and
// timestamp graph. swresample multiplies its matrix after normalization, so the
// gain must enter that resampler rather than a volume filter that requires float
// samples and can change the matrix's normalization. Only one resampler is used.
// Internal floating-point mixing also avoids fixed-point accumulator overflow
// for high gains; the original output format still selects normalization and
// saturating conversion to the encoded sample format.
func appendDownMixAudioFilter(p Plan, filter string) string {
	if p.ExecutionVersion != ExecutionVersion || !appliesDownMixBoost(p, p.Execution.DownMixAudioBoost) {
		return filter
	}
	options := "ochl=stereo:rematrix_volume=" + strconv.FormatFloat(p.Execution.DownMixAudioBoost, 'f', -1, 64) + ":tsf=fltp"
	parts := strings.Split(filter, ",")
	for index, part := range parts {
		if strings.HasPrefix(part, "aresample=") {
			parts[index] += ":" + options
			return strings.Join(parts, ",")
		}
	}
	if filter != "" {
		filter += ","
	}
	return filter + "aresample=" + options
}
