package eventsbus

import (
	"github.com/google/uuid"
)

var ebus *Bus

func init() {
	ebus = newEventsBus()
	go ebus.handleEvents()
}

func GlobalEventBus() (*Bus, string) {
	// return events bus with subscriberID
	return ebus, uuid.NewString()
}
