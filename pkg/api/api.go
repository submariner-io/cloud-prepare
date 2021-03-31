package api

type ReportingStatus int

const (
	Started ReportingStatus = iota
	Succeeded
	Failed
)

// Reporter is responsible for reporting back on the progress of the cloud preparation
type Reporter interface {
	// Started will report that an operation started on the cloud
	Started(message string, args ...interface{})

	// Succeeded will report that the last operation on the cloud has succeded
	Succeeded(message string, args ...interface{})

	// Failed will report that the last operation on the cloud has failed
	Failed(errs ...error)
}

// PortSpec is a specification of port+protocol to open
type PortSpec struct {
	Port     uint16
	Protocol string
}

type PrepareForSubmarinerInput struct {
	// List of ports to open inside the cluster for proper communication between Submariner services
	InternalPorts []PortSpec

	// List of ports to open externally so that Submariner can reach and be reached by other Submariners
	PublicPorts []PortSpec
}

// Cloud is a potential cloud for installing Submariner on
type Cloud interface {
	// PrepareForSubmariner will prepare the cloud for Submariner to operate on
	PrepareForSubmariner(input PrepareForSubmarinerInput, reporter Reporter) error

	// CleanupAfterSubmariner will clean up the cloud after Submariner is removed
	CleanupAfterSubmariner(reporter Reporter) error
}
