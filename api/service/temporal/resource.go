package temporal

import "go.temporal.io/sdk/client"

// ClientResource represents an acquired Temporal client resource
// with task queue name prefixing support
type ClientResource struct {
	// Client is the underlying Temporal SDK client
	Client client.Client

	// TQPrefix is the task queue name prefix configured for this client
	TQPrefix string
}

// GetTaskQueueName applies the configured prefix to a task queue name
func (r *ClientResource) GetTaskQueueName(queueName string) string {
	if r.TQPrefix == "" {
		return queueName
	}
	return r.TQPrefix + queueName
}
