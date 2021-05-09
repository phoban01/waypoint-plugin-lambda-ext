PLUGIN_NAME=lambda-ext

ifndef _ARCH
_ARCH := $(shell ./print_arch)
export _ARCH
endif

.PHONY: all

all: protos build

# Generate the Go code from Protocol Buffer definitions
protos:
	@echo ""
	@echo "Build Protos"

	protoc -I . --go_out=plugins=grpc:. --go_opt=paths=source_relative ./platform/output.proto

# Builds the plugin on your local machine
build:
	@echo ""
	@echo "Compile Plugin"

	# Clear the output
	rm -rf ./bin

	GOOS=linux GOARCH=amd64 go build -o ./bin/linux_amd64/waypoint-plugin-${PLUGIN_NAME} ./main.go

# Install the plugin locally
install:
	@echo ""
	@echo "Installing Plugin"

	cp ./bin/${_ARCH}_amd64/waypoint-plugin-${PLUGIN_NAME}* ${HOME}/.config/waypoint/plugins/

# Zip the built plugin binaries
zip:
	zip -j ./bin/waypoint-plugin-${PLUGIN_NAME}_linux_amd64.zip ./bin/linux_amd64/waypoint-plugin-${PLUGIN_NAME}

# Build the plugin using a Docker container
build-docker:
	rm -rf ./releases
	DOCKER_BUILDKIT=1 docker build --output releases --progress=plain .
