package __transform

// YAML representation of the http server entry
const httpServerYAML = `
name: web_server
kind: http.server
meta:
  server_id: "default"
  comment: "Default http server."

# Server configuration
listen: ":8080"
http:
  idle_timeout: 60s

lifecycle:
  auto_start: true
  restart:
    delay: 5s
    max_attempts: 3
`

//
//// Mock component to simulate a component that handles registry events
//type mockComponent struct {
//	handledEvents []events.Event
//}
//
//func (m *mockComponent) handleEvent(evt events.Event) {
//	m.handledEvents = append(m.handledEvents, evt)
//}
//
//func createTestTranscoder() payload.Transcoder {
//	tr := transcoder.NewTranscoder()
//	json.Register(tr)
//	yaml.Register(tr)
//
//	return tr
//}
//
//func TestBusRunner_CreateHttpServerEntry_WithYAML_AndEvents(t *testing.T) {
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	bus := eventbus.NewBus(zap.NewNop())
//	busRunner := runner.NewBusRunner(bus, zap.NewNop())
//
//	// 2. Create ChangeSet for publishing
//	//changeSet := registry.ChangeSet{
//	//	{
//	//		Kind:  registry.Create,
//	//		Entry: decodedEntry,
//	//	},
//	//}
//
//	// 3. Create and attach a mock component to listen for events
//	component := &mockComponent{}
//	listener, err := eventbus.NewSubscriber(ctx, bus, registry.System, "", component.handleEvent)
//	require.NoError(t, err)
//	defer listener.Close()
//
//	// 4. Publish the ChangeSet (Transition)
//	_, err = busRunner.Transition(ctx, registry.State{}, changeSet)
//	require.NoError(t, err)
//
//	// 5. Wait for events to be processed (give it some time)
//	time.Sleep(100 * time.Millisecond)
//
//	// 6. Assertions
//	// a. Verify that the mock component received events
//	assert.NotEmpty(t, component.handledEvents, "Mock component should have received events")
//
//	// b. Check for Begin and Commit events
//	var beginEvent, commitEvent bool
//	for _, evt := range component.handledEvents {
//		if evt.Kind == registry.Begin {
//			beginEvent = true
//		}
//		if evt.Kind == registry.Commit {
//			commitEvent = true
//		}
//	}
//	assert.True(t, beginEvent, "Begin event should have been received")
//	assert.True(t, commitEvent, "Commit event should have been received")
//
//	// c. Verify that the entry was created (check for Create event with correct data)
//	var createEventFound bool
//	for _, evt := range component.handledEvents {
//		if evt.Kind == registry.Create && evt.Data.(registry.Entry).ID == "web_server" {
//			createEventFound = true
//			createdEntry := evt.Data.(registry.Entry)
//
//			assert.Equal(t, "web_server", createdEntry.ID)
//			assert.Equal(t, "http.server", createdEntry.Kind)
//			assert.Equal(t, map[string]string{
//				"server_id": "default",
//				"comment":   "Default http server.",
//			}, createdEntry.Meta)
//
//			// Verify the data payload
//			data, ok := createdEntry.Data.Data().(map[string]any)
//			require.True(t, ok)
//			assert.Equal(t, ":8080", data["listen"])
//			assert.Equal(t, map[string]any{"idle_timeout": "60s"}, data["http"])
//			assert.Equal(t, map[string]any{
//				"auto_start": true,
//				"restart": map[string]any{
//					"delay":        "5s",
//					"max_attempts": 3,
//				},
//			}, data["lifecycle"])
//			break
//		}
//	}
//	assert.True(t, createEventFound, "Create event for 'web_server' should have been received")
//}
