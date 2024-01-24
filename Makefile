BASE_BRANCH ?= release-0.17
export BASE_BRANCH

ifneq (,$(DAPPER_HOST_ARCH))

# Running in Dapper

include $(SHIPYARD_DIR)/Makefile.inc

# Generated files

GO ?= go
MOCKGEN := $(shell $(GO) env GOPATH)/bin/mockgen
MOCKGEN_VERSION := v1.6.0

$(MOCKGEN):
	$(GO) install github.com/golang/mock/mockgen@$(MOCKGEN_VERSION)

pkg/aws/client/fake/client.go: pkg/aws/client/client.go | $(MOCKGEN)
	cd pkg/aws/client && $(GO) generate

pkg/gcp/client/fake/client.go: pkg/gcp/client/client.go | $(MOCKGEN)
	cd pkg/gcp/client && $(GO) generate

pkg/ocp/fake/machineset.go: pkg/ocp/machinesets.go | $(MOCKGEN)
	cd pkg/ocp && $(GO) generate

unit: pkg/aws/client/fake/client.go pkg/gcp/client/fake/client.go pkg/ocp/fake/machineset.go

else

# Not running in Dapper

Makefile.dapper:
	@echo Downloading $@
	@curl -sfLO https://raw.githubusercontent.com/submariner-io/shipyard/$(BASE_BRANCH)/$@

include Makefile.dapper

endif

# Disable rebuilding Makefile
Makefile Makefile.inc: ;
