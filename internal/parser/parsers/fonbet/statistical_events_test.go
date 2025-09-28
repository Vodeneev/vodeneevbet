package fonbet

import (
	"testing"
)

func TestUnifiedEventParsing(t *testing.T) {
	parser := NewJSONParser()
	
	// Test data with various events (main match + statistical events)
	jsonData := []byte(`{
		"events": [
			{
				"id": 1,
				"name": "Main Match",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 1,
				"rootKind": 1,
				"level": 0,
				"team1": "Team A",
				"team2": "Team B"
			},
			{
				"id": 2,
				"name": "Corners",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 400100,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			},
			{
				"id": 3,
				"name": "Yellow Cards",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 400200,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			},
			{
				"id": 4,
				"name": "Fouls",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 400300,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			},
			{
				"id": 5,
				"name": "Shots on Target",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 400400,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			},
			{
				"id": 6,
				"name": "Offsides",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 400500,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			},
			{
				"id": 7,
				"name": "Throw-ins",
				"startTime": 1640995200,
				"sportId": 1,
				"kind": 401000,
				"rootKind": 400000,
				"level": 1,
				"parentId": 1
			}
		]
	}`)
	
	// Test parsing all events (unified approach)
	events, err := parser.ParseEvents(jsonData)
	if err != nil {
		t.Fatalf("Failed to parse events: %v", err)
	}
	
	// Should get all 7 events (1 main match + 6 statistical events)
	if len(events) != 7 {
		t.Errorf("Expected 7 events, got %d", len(events))
	}
	
	// Check that all events have the correct Kind values
	expectedKinds := []int64{1, 400100, 400200, 400300, 400400, 400500, 401000}
	for i, event := range events {
		if event.Kind != expectedKinds[i] {
			t.Errorf("Event %d: expected Kind %d, got %d", i, expectedKinds[i], event.Kind)
		}
	}
	
	// Test parsing all statistical events (still available for backward compatibility)
	statisticalEvents, err := parser.ParseAllStatisticalEvents(jsonData)
	if err != nil {
		t.Fatalf("Failed to parse statistical events: %v", err)
	}
	
	// Check that we found all types of statistical events
	expectedTypes := []string{"corners", "yellow_cards", "fouls", "shots_on_target", "offsides", "throw_ins"}
	for _, eventType := range expectedTypes {
		if events, exists := statisticalEvents[eventType]; !exists || len(events) == 0 {
			t.Errorf("Expected to find %s events, but found none", eventType)
		} else {
			t.Logf("Found %d %s events", len(events), eventType)
		}
	}
}

func TestStatisticalEventTypeDetection(t *testing.T) {
	parser := NewJSONParser()
	
	// Test corner event
	cornerEvent := FonbetAPIEvent{
		ID:       2,
		Kind:     400100,
		RootKind: 400000,
	}
	
	if !parser.isCornerEvent(cornerEvent) {
		t.Error("Expected corner event to be detected as corner")
	}
	
	// Test yellow card event
	yellowCardEvent := FonbetAPIEvent{
		ID:       3,
		Kind:     400200,
		RootKind: 400000,
	}
	
	if !parser.isYellowCardEvent(yellowCardEvent) {
		t.Error("Expected yellow card event to be detected as yellow card")
	}
	
	// Test foul event
	foulEvent := FonbetAPIEvent{
		ID:       4,
		Kind:     400300,
		RootKind: 400000,
	}
	
	if !parser.isFoulEvent(foulEvent) {
		t.Error("Expected foul event to be detected as foul")
	}
	
	// Test shots on target event
	shotsEvent := FonbetAPIEvent{
		ID:       5,
		Kind:     400400,
		RootKind: 400000,
	}
	
	if !parser.isShotsOnTargetEvent(shotsEvent) {
		t.Error("Expected shots on target event to be detected as shots on target")
	}
	
	// Test offside event
	offsideEvent := FonbetAPIEvent{
		ID:       6,
		Kind:     400500,
		RootKind: 400000,
	}
	
	if !parser.isOffsideEvent(offsideEvent) {
		t.Error("Expected offside event to be detected as offside")
	}
	
	// Test throw-in event
	throwInEvent := FonbetAPIEvent{
		ID:       7,
		Kind:     401000,
		RootKind: 400000,
	}
	
	if !parser.isThrowInEvent(throwInEvent) {
		t.Error("Expected throw-in event to be detected as throw-in")
	}
}

func TestGetStatisticalEventType(t *testing.T) {
	parser := NewJSONParser()
	
	testCases := []struct {
		kind     int64
		expected string
	}{
		{400100, "corners"},
		{400200, "yellow_cards"},
		{400300, "fouls"},
		{400400, "shots_on_target"},
		{400500, "offsides"},
		{401000, "throw_ins"},
		{999999, ""}, // Unknown kind
	}
	
	for _, tc := range testCases {
		event := FonbetAPIEvent{Kind: tc.kind}
		result := parser.getStatisticalEventType(event)
		if result != tc.expected {
			t.Errorf("Expected %s for kind %d, got %s", tc.expected, tc.kind, result)
		}
	}
}
