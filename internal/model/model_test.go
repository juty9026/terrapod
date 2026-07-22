package model

import "testing"

func TestResourceValidate(t *testing.T) {
	tests := []struct {
		name    string
		input   Resource
		wantErr bool
	}{
		{"valid", Resource{ID: "core.ripgrep", Type: ResourcePackage, VersionPolicy: VersionTracked}, false},
		{"missing id", Resource{Type: ResourcePackage, VersionPolicy: VersionTracked}, true},
		{"bad id", Resource{ID: "Rip Grep", Type: ResourcePackage, VersionPolicy: VersionTracked}, true},
		{"bad policy", Resource{ID: "core.ripgrep", Type: ResourcePackage, VersionPolicy: "rolling"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.input.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestProfileSupported(t *testing.T) {
	for _, p := range []Profile{ProfileMacOSTerminal, ProfileVPSShell} {
		if !p.Supported() {
			t.Fatalf("expected %q to be supported", p)
		}
	}
	if Profile("linux").Supported() {
		t.Fatal("generic linux must not be supported")
	}
}
