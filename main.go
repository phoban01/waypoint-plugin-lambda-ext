package main

import (
	sdk "github.com/hashicorp/waypoint-plugin-sdk"
	"github.com/phoban01/lambda-ext/platform"
)

func main() {
	sdk.Main(sdk.WithComponents(
		&platform.Platform{},
	))
}
