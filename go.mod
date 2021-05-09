module github.com/phoban01/lambda-ext

go 1.14

require (
	github.com/aws/aws-sdk-go v1.38.36
	github.com/golang/protobuf v1.4.3
	github.com/hashicorp/go-hclog v0.14.1
	github.com/hashicorp/waypoint v0.3.1
	github.com/hashicorp/waypoint-plugin-sdk v0.0.0-20210319163606-c48e1a6cba30
	github.com/pkg/errors v0.9.1
)

// replace github.com/hashicorp/waypoint-plugin-sdk => ../../waypoint-plugin-sdk
