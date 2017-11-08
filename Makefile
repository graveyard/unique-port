include golang.mk
.DEFAULT_GOAL := test # override default goal set in library makefile

SHELL := /bin/bash
PKGS := $(shell go list ./... | grep -v /vendor)
.PHONY: $(PKGS) test clean uniqueport

$(eval $(call golang-version-check,1.7))

export TEST_DYNAMODB_REGION ?= us-west-2
export TEST_DYNAMODB_ENDPOINT ?= dynamodb.us-west-2.amazonaws.com
export TEST_DYNAMODB_LOCKTABLE ?= custom-cf-resource-uniqueport-LockTable-CBHO33P21MBO
export TEST_DYNAMODB_PORTSTABLE ?= custom-cf-resource-uniqueport-PortsTable-1BVFH9XOQTV10
test: $(PKGS)




$(PKGS): golang-test-all-deps
	$(call golang-test-all,$@)

clean:
	rm -f uniqueport uniqueport.zip

uniqueport: *.go
	GOOS=linux GOARCH=amd64 go build -a

uniqueport.zip: lambda.js uniqueport
	zip uniqueport.zip lambda.js uniqueport

release: uniqueport.zip
	aws s3 cp uniqueport.zip s3://$$PUBLIC_AWS_BUCKET/uniqueport.zip --acl public-read

