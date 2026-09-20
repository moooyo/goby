#ifndef GOBY_INTRO_FINGERPRINT_CONFIG_H
#define GOBY_INTRO_FINGERPRINT_CONFIG_H

// This dedicated build accepts input already at the algorithm's native rate.
// Optional upstream libraries remain undefined because FFT selection uses
// #ifdef, whereas the audio processor tests the numeric resampler switch.
#define USE_KISSFFT 1
#define USE_INTERNAL_AVRESAMPLE 0
#define HAVE_ROUND 1
#define HAVE_LRINTF 1

#endif
