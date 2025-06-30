package adapter

// ConnClosedError is returned when operations are attempted on a closed connection.
type ConnClosedError struct{}

// PublisherConfEmptyError indicates that a nil or empty publisher configuration
// was provided when creating a new publisher.
type PublisherConfEmptyError struct{}

// ConsumerConfEmptyError indicates that a nil or empty consumer configuration
// was provided when creating a new consumer.
type ConsumerConfEmptyError struct{}

// PublisherClosedError is returned when publishing is attempted on a closed publisher.
type PublisherClosedError struct{}

// ConsumerClosedError is returned when consuming is attempted after the consumer has been closed.
type ConsumerClosedError struct{}

// Error implements the error interface for ConnClosedError.
// It indicates the client explicitly closed the connection.
func (e ConnClosedError) Error() string {
	return "connection closed by client"
}

// Error implements the error interface for ConsumerConfEmptyError.
// It notifies that consumer configuration was not provided.
func (ConsumerConfEmptyError) Error() string {
	return "empty consumer config passed, unable to create"
}

// Error implements the error interface for PublisherConfEmptyError.
// It notifies that publisher configuration was not provided.
func (PublisherConfEmptyError) Error() string {
	return "empty publisher config passed, unable to create"
}

// Error implements the error interface for PublisherClosedError.
// It signals that the publisher has already been closed.
func (PublisherClosedError) Error() string {
	return "publisher already closed, unable to provide"
}

// Error implements the error interface for ConsumerClosedError.
// It signals that the consumer has already been closed.
func (ConsumerClosedError) Error() string {
	return "consumer already closed, unable to provide"
}
