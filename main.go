package main

import (
	sdk "github.com/hashicorp/waypoint-plugin-sdk"
	"github.com/phoban01/lambda-ext/platform"
	"github.com/phoban01/lambda-ext/release"
)

func main() {
	sdk.Main(sdk.WithComponents(
		&platform.Platform{},
		&release.ReleaseManager{},
	))
}
