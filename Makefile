BASE_BRANCH ?= release-0.19
export BASE_BRANCH

ifneq (,$(DAPPER_HOST_ARCH))

# Running in Dapper

include $(SHIPYARD_DIR)/Makefile.inc

# Generated files

GO ?= go
MOCKGEN := $(CURDIR)/bin/mockery

$(MOCKGEN):
	mkdir -p $(@D) && $(GO) -C tools build -o $@ github.com/vektra/mockery/v2

pkg/aws/client/fake/client.go: pkg/aws/client/client.go pkg/aws/client/.mockery.yaml | $(MOCKGEN)
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/gcp/client/fake/client.go: pkg/gcp/client/client.go pkg/gcp/client/.mockery.yaml | $(MOCKGEN)
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/ocp/fake/machineset.go: pkg/ocp/machinesets.go pkg/ocp/.mockery.yaml | $(MOCKGEN)
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

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
