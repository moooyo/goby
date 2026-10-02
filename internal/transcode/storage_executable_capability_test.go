package transcode

import (
	"encoding/json"
	"testing"
)

func TestFixedExecutableCapabilitiesCannotBeWireAuthority(t *testing.T) {
	for _, value := range []any{&fixedPoolExecutablePreparation{}, &fixedPoolExecutableCapability{}, &fixedPoolExecutableUse{}, &fixedPoolNativeExecutable{}} {
		if _, err := json.Marshal(value); err == nil {
			t.Fatal("opaque executable authority serialized into JSON")
		}
		if err := json.Unmarshal([]byte(`{"approved":true,"Role":"executable","issuer":{},"digest":"self-asserted"}`), value); err == nil {
			t.Fatal("JSON minted executable authority")
		}
	}
	for _, value := range []any{fixedPoolExecutablePreparation{}, fixedPoolExecutableCapability{}, fixedPoolExecutableUse{}, fixedPoolNativeExecutable{}} {
		if _, err := json.Marshal(value); err == nil {
			t.Fatal("opaque capability value serialized into JSON")
		}
	}
	if _, err := (&fixedPoolExecutableCapability{}).duplicate(); err == nil {
		t.Fatal("zero capability acquired a descriptor use")
	}
	if _, err := (&fixedPoolExecutableCapability{issuer: struct{}{}}).duplicate(); err == nil {
		t.Fatal("foreign issuer acquired a descriptor use")
	}
	if err := (&fixedPoolExecutableUse{}).close(); err == nil {
		t.Fatal("zero executable use retired ownership")
	}
	if err := (&fixedPoolNativeExecutable{}).close(); err == nil {
		t.Fatal("zero native bridge retired ownership")
	}
}
