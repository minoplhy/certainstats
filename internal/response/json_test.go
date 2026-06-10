package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJSON(t *testing.T) {
	t.Run("writes correct content type, status, and body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		data := map[string]string{"foo": "bar"}

		JSON(rec, http.StatusCreated, data)

		if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
			t.Errorf("expected content type application/json, got %q", contentType)
		}

		if rec.Code != http.StatusCreated {
			t.Errorf("expected status %d, got %d", http.StatusCreated, rec.Code)
		}

		var decoded map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
			t.Fatalf("failed to decode response body: %v", err)
		}

		if decoded["foo"] != "bar" {
			t.Errorf("expected decoded foo to be %q, got %q", "bar", decoded["foo"])
		}
	})

	t.Run("handles nil data without panic", func(t *testing.T) {
		rec := httptest.NewRecorder()

		JSON(rec, http.StatusNoContent, nil)

		if rec.Code != http.StatusNoContent {
			t.Errorf("expected status %d, got %d", http.StatusNoContent, rec.Code)
		}

		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body, got %q", rec.Body.String())
		}
	})
}

func TestError(t *testing.T) {
	rec := httptest.NewRecorder()
	errorMessage := "something went wrong"

	Error(rec, http.StatusBadRequest, errorMessage)

	if contentType := rec.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected content type application/json, got %q", contentType)
	}

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var decoded map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
		t.Fatalf("failed to decode error body: %v", err)
	}

	if decoded["error"] != errorMessage {
		t.Errorf("expected error field to be %q, got %q", errorMessage, decoded["error"])
	}
}

func TestSuccess(t *testing.T) {
	t.Run("structures success with message and data", func(t *testing.T) {
		rec := httptest.NewRecorder()
		data := map[string]int{"count": 5}
		message := "retrieved counts"

		Success(rec, message, data)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var decoded map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if decoded["message"] != message {
			t.Errorf("expected message to be %q, got %q", message, decoded["message"])
		}

		dataField, ok := decoded["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected data field to be a map, got %T", decoded["data"])
		}

		if dataField["count"] != 5.0 { // json numbers are floats when decoding to interface{}
			t.Errorf("expected count to be 5, got %v", dataField["count"])
		}
	})

	t.Run("omits message or data when empty or nil", func(t *testing.T) {
		rec := httptest.NewRecorder()

		Success(rec, "", nil)

		var decoded map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&decoded); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if _, hasMessage := decoded["message"]; hasMessage {
			t.Errorf("did not expect message field to be present")
		}

		if _, hasData := decoded["data"]; hasData {
			t.Errorf("did not expect data field to be present")
		}
	})
}
