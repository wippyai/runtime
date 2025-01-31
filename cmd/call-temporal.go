package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"go.temporal.io/sdk/client"
)

func executeWorkflow(c client.Client, wg *sync.WaitGroup, index int) {
	defer wg.Done()

	// Configure workflow options with unique ID
	options := client.StartWorkflowOptions{
		ID:                 fmt.Sprintf("stab-workflow-%d-%s", index, time.Now().Format("2006-01-02-15-04-05")),
		TaskQueue:          "wippy_demos",
		WorkflowRunTimeout: time.Minute * 10,
	}

	// Start workflow
	we, err := c.ExecuteWorkflow(context.Background(), options, "demo_workflow")
	if err != nil {
		log.Printf("Failed to execute workflow %d: %v\n", index, err)
		return
	}

	// Send 10 signals with 100ms intervals
	for i := 0; i < 50; i++ {
		err = c.SignalWorkflow(context.Background(), we.GetID(), we.GetRunID(), "signal", fmt.Sprintf("Signal %d", i))
		if err != nil {
			log.Printf("Failed to send signal %d to workflow %d: %v\n", i, index, err)
			continue
		}
		log.Printf("Sent signal %d to workflow %d\n", i, index)
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for workflow completion
	err = we.Get(context.Background(), nil)
	if err != nil {
		log.Printf("Workflow %d failed: %v\n", index, err)
		return
	}

	log.Printf("Workflow %d completed! WorkflowID: %s RunID: %s\n", index, we.GetID(), we.GetRunID())
}

func main() {
	// Create temporal client
	c, err := client.Dial(client.Options{
		HostPort: "localhost:7233",
	})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	// Create wait group to track workflow completion
	var wg sync.WaitGroup

	// Launch workflow
	wg.Add(1)
	go executeWorkflow(c, &wg, 0)

	// Wait for workflow to complete
	wg.Wait()
	log.Println("All workflows completed!")
}
