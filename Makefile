SHELL=/bin/bash

.PHONY: all
all:
	$(MAKE) lint
	$(MAKE) gen-swagger
	$(MAKE) gen-src
	$(MAKE) gen-proto
	$(MAKE) update-mod
	$(MAKE) gazelle
	$(MAKE) build
	$(MAKE) test
	$(MAKE) dev-docker-build

.PHONY: gen-proto
gen-proto:
	@pushd ./proto && \
	protoc --go_out=./src/ --go_opt=paths=source_relative \
        --go-grpc_out=./src/ --go-grpc_opt=paths=source_relative \
		proto/hash.proto && \
	popd

.PHONY: update-mod
update-mod:
	@pushd ./cocotola-api/ && \
		go get -u ./... && \
	popd
	@pushd ./cocotola-synthesizer-api/ && \
		go get -u ./... && \
	popd
	@pushd ./cocotola-tatoeba-api/ && \
		go get -u ./... && \
	popd
	@pushd ./cocotola-translator-api/ && \
		go get -u ./... && \
	popd

work-init:
	@go work init
	@go work use -r .

gazelle:
	sudo chmod 777 -R docker/development
	@bazel run //:gazelle -- update-repos -from_file ./go.work
	@bazel run //:gazelle

build:
	@bazel build //...

test:
	@bazel test //... --test_output=all --test_timeout=60

docker-build:
	bazel build //src:go_image

docker-run:
	bazel run //src:go_image
