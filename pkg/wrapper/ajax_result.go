package wrapper

import (
	"bytes"
	"encoding/json"
	"errors"
)

// validateAjaxResult rejects explicit errors before callers consume JSON content.
// Mutations additionally require a positive status or success value.
func validateAjaxResult(body []byte, operation string, requirePositive bool) error {
	var result struct {
		Status  string          `json:"status"`
		Success json.RawMessage `json:"success"`
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Errors  []OGameError    `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if len(result.Errors) > 0 {
		if result.Errors[0].Message != "" {
			return errors.New(result.Errors[0].Message)
		}
		return errors.New(operation + " failed")
	}
	success := string(bytes.TrimSpace(result.Success))
	errorValue := string(bytes.TrimSpace(result.Error))
	if (result.Status != "" && result.Status != "success") ||
		(success != "" && success != "true") ||
		(errorValue != "" && errorValue != "false" && errorValue != "null") ||
		(requirePositive && result.Status != "success" && success != "true") {
		if result.Message != "" {
			return errors.New(result.Message)
		}
		return errors.New(operation + " failed")
	}
	return nil
}
