package service_test

import (
	"testing"

	"github.com/whaleshell/whaleshell-cli/internal/service"
)

func TestGuestGatewayURL(t *testing.T) {
	cases := map[string]string{
		"":                                     "",
		"http://127.0.0.1:7443":                "http://host.whaleshell.internal:7443",
		"http://localhost:7443":                "http://host.whaleshell.internal:7443",
		"http://host.whaleshell.internal:7443": "http://host.whaleshell.internal:7443",
		"http://host.docker.internal:7443":     "http://host.whaleshell.internal:7443",
	}
	for in, want := range cases {
		if got := service.GuestGatewayURL(in); got != want {
			t.Fatalf("GuestGatewayURL(%q)=%q want %q", in, got, want)
		}
	}
}
