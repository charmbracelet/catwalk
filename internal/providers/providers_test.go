package providers

import (
	"testing"
)

func TestXiaomiProvider(t *testing.T) {
	providers := GetAll()

	var xiaomiFound bool
	for _, p := range providers {
		if p.ID == "xiaomi" {
			xiaomiFound = true
			
			if p.Name != "Xiaomi" {
				t.Errorf("Expected name 'Xiaomi', got '%s'", p.Name)
			}
			
			if p.Type != "xiaomi" {
				t.Errorf("Expected type 'xiaomi', got '%s'", p.Type)
			}
			
			if p.APIEndpoint != "https://api.xiaomimimo.com/v1" {
				t.Errorf("Expected API endpoint 'https://api.xiaomimimo.com/v1', got '%s'", p.APIEndpoint)
			}
			
			if p.DefaultLargeModelID != "mimo-v2-flash" {
				t.Errorf("Expected default large model 'mimo-v2-flash', got '%s'", p.DefaultLargeModelID)
			}
			
			if p.DefaultSmallModelID != "mimo-v2-flash" {
				t.Errorf("Expected default small model 'mimo-v2-flash', got '%s'", p.DefaultSmallModelID)
			}
			
			if len(p.Models) != 1 {
				t.Errorf("Expected 1 model, got %d", len(p.Models))
			}
			
			if len(p.Models) > 0 {
				model := p.Models[0]
				if model.ID != "mimo-v2-flash" {
					t.Errorf("Expected model ID 'mimo-v2-flash', got '%s'", model.ID)
				}
				if model.Name != "Mimo V2 Flash" {
					t.Errorf("Expected model name 'Mimo V2 Flash', got '%s'", model.Name)
				}
				if !model.CanReason {
					t.Error("Expected model to have CanReason = true")
				}
				if model.ContextWindow != 256000 {
					t.Errorf("Expected context window 256000, got %d", model.ContextWindow)
				}
			}
			
			break
		}
	}
	
	if !xiaomiFound {
		t.Error("Xiaomi provider not found in provider registry")
	}
}