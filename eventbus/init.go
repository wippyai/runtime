package eventsbus

import (
	"github.com/google/uuid"
)

var evBus *Bus

func init() {
	evBus = newEventsBus()
	go evBus.handleEvents()
}

func NewEventBus() (*Bus, string) {
	// return events bus with subscriberID
	return evBus, uuid.NewString()
}
