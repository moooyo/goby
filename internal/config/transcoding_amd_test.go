package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestTranscodingAMDDeviceAuthorization(t *testing.T) {
	transcodingConfigEnvironment(t)
	t.Setenv("GOBY_ALLOWED_AMD_DEVICES", "/dev/dri/renderD129,/dev/dri/renderD255")
	t.Setenv("GOBY_HW_DECODER", "vaapi")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	devices, err := cfg.Transcoding.AuthorizedAMDDevices()
	if err != nil || !slices.Equal(devices, []string{"/dev/dri/renderD129", "/dev/dri/renderD255", "/dev/dri/renderD128"}) {
		t.Fatalf("startup authorization union = %v, %v", devices, err)
	}
	if cfg.Transcoding.AllowedAMDDeviceCount != 2 || cfg.Transcoding.AllowedAMDDevices[2] != "" {
		t.Fatalf("implicit compatibility authorization changed the explicit list: %+v", cfg.Transcoding)
	}
	devices[0] = "/dev/dri/renderD200"
	again, err := cfg.Transcoding.AuthorizedAMDDevices()
	if err != nil || again[0] != "/dev/dri/renderD129" {
		t.Fatal("returned authorization list aliases startup configuration")
	}
	t.Setenv("GOBY_HW_DEVICE", "/dev/dri/renderD129")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	devices, err = cfg.Transcoding.AuthorizedAMDDevices()
	if err != nil || len(devices) != 2 {
		t.Fatalf("overlapping startup device was not merged: %v, %v", devices, err)
	}
}

func TestTranscodingAMDDeviceAuthorizationRejectsMalformedLists(t *testing.T) {
	transcodingConfigEnvironment(t)
	for _, value := range []string{
		" ", ",", "/dev/dri/renderD128,", ",/dev/dri/renderD128", "/dev/dri/renderD128,,/dev/dri/renderD129",
		"/dev/dri/renderD128,/dev/dri/renderD128", " /dev/dri/renderD128", "/dev/dri/renderD128 ",
		"/dev/dri/renderD128, /dev/dri/renderD129", "/dev/dri/renderD128\n", "/dev/dri/renderD127", "/dev/dri/renderD256",
		"/dev/dri/renderD0128", "/dev/dri/renderD+128", "/dev/dri/renderD128/../renderD129", "/dev/dri/card0", "/tmp/renderD128",
		"/dev/dri/renderD128,/dev/dri/renderD129,/dev/dri/renderD130,/dev/dri/renderD131,/dev/dri/renderD132,/dev/dri/renderD133,/dev/dri/renderD134,/dev/dri/renderD135,/dev/dri/renderD136",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GOBY_ALLOWED_AMD_DEVICES", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_ALLOWED_AMD_DEVICES") {
				t.Fatalf("invalid list returned %v", err)
			}
		})
	}
}

func TestTranscodingAMDDeviceAuthorizationBoundIncludesStartupDevice(t *testing.T) {
	transcodingConfigEnvironment(t)
	t.Setenv("GOBY_ALLOWED_AMD_DEVICES", "/dev/dri/renderD129,/dev/dri/renderD130,/dev/dri/renderD131,/dev/dri/renderD132,/dev/dri/renderD133,/dev/dri/renderD134,/dev/dri/renderD135,/dev/dri/renderD136")
	t.Setenv("GOBY_HW_ENCODER", "vaapi")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_ALLOWED_AMD_DEVICES") {
		t.Fatalf("ninth implicit startup device returned %v", err)
	}
	t.Setenv("GOBY_HW_DEVICE", "/dev/dri/renderD129")
	if _, err := Load(); err != nil {
		t.Fatalf("eight unique combined devices rejected: %v", err)
	}
}

func TestTranscodingAMDDeviceAuthorizationDoesNotDiscoverDevices(t *testing.T) {
	for _, hardware := range []transcode.Hardware{{}, {Decode: "software", Encode: "software"}, {Encode: "qsv", Device: "/dev/dri/renderD128"}, {Encode: "nvenc", Device: "0"}} {
		devices, err := (TranscodingConfig{Hardware: hardware}).AuthorizedAMDDevices()
		if err != nil || len(devices) != 0 {
			t.Fatalf("non-VAAPI startup configuration authorized AMD devices: %v, %v", devices, err)
		}
	}
	for _, cfg := range []TranscodingConfig{
		{AllowedAMDDeviceCount: -1}, {AllowedAMDDeviceCount: 9},
		{AllowedAMDDeviceCount: 1}, {AllowedAMDDevices: [8]string{"/dev/dri/renderD128"}},
		{AllowedAMDDevices: [8]string{"/dev/dri/renderD128", "/dev/dri/renderD129"}, AllowedAMDDeviceCount: 1},
	} {
		if _, err := cfg.AuthorizedAMDDevices(); err == nil {
			t.Fatalf("noncanonical fixed authorization storage accepted: %+v", cfg)
		}
	}
}
