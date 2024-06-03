BASE_BRANCH ?= release-0.18
export BASE_BRANCH

ifneq (,$(DAPPER_HOST_ARCH))

# Running in Dapper

include $(SHIPYARD_DIR)/Makefile.inc

# Generated files

GO ?= go
MOCKGEN := $(CURDIR)/bin/mockgen

$(MOCKGEN):
	mkdir -p $(@D) && $(GO) build -o $@ go.uber.org/mock/mockgen

pkg/aws/client/fake/client.go: pkg/aws/client/client.go | $(MOCKGEN)
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/gcp/client/fake/client.go: pkg/gcp/client/client.go | $(MOCKGEN)
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/ocp/fake/machineset.go: pkg/ocp/machinesets.go | $(MOCKGEN)
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
