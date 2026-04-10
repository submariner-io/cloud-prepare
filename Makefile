BASE_BRANCH ?= devel
export BASE_BRANCH

ifneq (,$(DAPPER_HOST_ARCH))

# Running in Dapper

include $(SHIPYARD_DIR)/Makefile.inc

# Generated files

GO ?= go
MOCKGEN = $(shell $(GO) -C tools tool -n github.com/vektra/mockery/v3)

pkg/aws/client/fake/client.go: pkg/aws/client/client.go pkg/aws/client/.mockery.yaml
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/gcp/client/fake/client.go: pkg/gcp/client/client.go pkg/gcp/client/.mockery.yaml
	PATH=$(dir $(MOCKGEN)):$$PATH $(GO) -C $(<D) generate

pkg/ocp/fake/machineset.go: pkg/ocp/machinesets.go pkg/ocp/.mockery.yaml
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
