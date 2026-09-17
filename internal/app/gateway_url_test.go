package app_test

import (
	"testing"

	"github.com/zorneth/osg-cli/internal/app"
)

func TestGuestGatewayURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"http://127.0.0.1:7443":            "http://host.osg.internal:7443",
		"http://localhost:7443":            "http://host.osg.internal:7443",
		"http://host.osg.internal:7443":    "http://host.osg.internal:7443",
		"http://host.docker.internal:7443": "http://host.osg.internal:7443",
	}
	for in, want := range cases {
		if got := app.GuestGatewayURL(in); got != want {
			t.Fatalf("GuestGatewayURL(%q)=%q want %q", in, got, want)
		}
	}
}
