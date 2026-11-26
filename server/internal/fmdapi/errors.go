package fmdapi

import (
	"fmt"
	"net/http"
)

// Error is a FileMaker Data API error reply.
type Error struct {
	Status  int
	Code    string
	Message string
	// Front marks an error of FileMaker Server's Node.js front end, which
	// writes "message" before "code". The engine writes "code" first.
	Front bool
}

func (e *Error) Error() string { return e.Code + " " + e.Message }

// engineErrors maps each FileMaker error code the Data API engine answers
// with to its HTTP status and message, as FileMaker Server 26 sends them
// (testdata/dataapi). A message left empty depends on the request.
var engineErrors = map[string]struct {
	status  int
	message string
}{
	// -1 and 3 are not recorded: File4Base answers -1 when it fails
	// internally, and 3 for an endpoint it does not serve yet.
	"-1":   {http.StatusInternalServerError, "Unknown error"},
	"1":    {http.StatusInternalServerError, "User canceled action"},
	"3":    {http.StatusInternalServerError, "Command is unavailable"},
	"9":    {http.StatusUnauthorized, "Insufficient privileges"},
	"11":   {http.StatusInternalServerError, "Name is not valid"},
	"101":  {http.StatusInternalServerError, "Record is missing"},
	"102":  {http.StatusInternalServerError, "Field is missing"},
	"104":  {http.StatusInternalServerError, ""}, // "<parameter>:'<script>' is missing"
	"105":  {http.StatusInternalServerError, "Layout is missing"},
	"108":  {http.StatusInternalServerError, "Value list is missing"},
	"110":  {http.StatusInternalServerError, "Related tables are missing"},
	"111":  {http.StatusInternalServerError, "Field repetition is invalid"},
	"200":  {http.StatusInternalServerError, "Record access is denied"},
	"201":  {http.StatusInternalServerError, "Field cannot be modified"},
	"212":  {http.StatusUnauthorized, "Invalid user account or password"},
	"301":  {http.StatusInternalServerError, "Record is in use by another user"},
	"306":  {http.StatusInternalServerError, "Record modification ID does not match"},
	"400":  {http.StatusInternalServerError, "Find criteria are empty"},
	"401":  {http.StatusOK, "No records match the request"}, // a result, not an error: "response" comes first
	"408":  {http.StatusInternalServerError, "Specified field has inappropriate data type for this operation"},
	"413":  {http.StatusInternalServerError, "Specified field has inappropriate field type"},
	"500":  {http.StatusInternalServerError, "Date value does not meet validation entry options"},
	"502":  {http.StatusInternalServerError, "Number value does not meet validation entry options"},
	"503":  {http.StatusInternalServerError, "Value in field is not within the range specified in validation entry options"},
	"504":  {http.StatusInternalServerError, "Value in field is not unique, as required in validation entry options"},
	"506":  {http.StatusInternalServerError, "Value in field is not listed in the value list specified in validation entry option"},
	"507":  {http.StatusInternalServerError, "Value in field failed calculation test of validation entry option"},
	"508":  {http.StatusInternalServerError, "Invalid value entered in Find mode"},
	"509":  {http.StatusInternalServerError, "Field requires a valid value"},
	"511":  {http.StatusInternalServerError, "Value in field exceeds maximum field size"},
	"802":  {http.StatusInternalServerError, "Unable to open file"},
	"952":  {http.StatusUnauthorized, "Invalid FileMaker Data API token (*)"},
	"960":  {http.StatusInternalServerError, "Parameter is invalid"},
	"1708": {http.StatusInternalServerError, "Parameter value is invalid"},
}

// engineError is the engine's reply for code, with its catalogue message.
func engineError(code string) *Error {
	e, ok := engineErrors[code]
	if !ok || e.message == "" {
		panic(fmt.Sprintf("fmdapi: no catalogue message for FileMaker error %s", code))
	}
	return &Error{Status: e.status, Code: code, Message: e.message}
}

// frontError is a reply of FileMaker Server's front end.
func frontError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message, Front: true}
}

// The front end's replies File4Base reproduces.
var (
	errMethodNotAllowed        = frontError(http.StatusMethodNotAllowed, "1704", "Resource doesn't support the specified HTTP verb")
	errAuthorizationRequired   = frontError(http.StatusBadRequest, "10", "Request validation failed: Parameter (Authorization) is required")
	errBasicInvalid            = frontError(http.StatusUnauthorized, "212", "Authorization header 'Basic' invalid.")
	errLoginCredentialsMissing = frontError(http.StatusUnauthorized, "10", "HTTP Authorization header or OAuth headers are missing.")
	errAuthorizationInvalid    = frontError(http.StatusUnauthorized, "960", "HTTP Authorization header invalid")
)

// contentTypeError is the request validator's reply to a body of the
// wrong media type.
func contentTypeError(got, valid string) *Error {
	return frontError(http.StatusBadRequest, "1708", "Invalid content type ("+got+").  These are valid: "+valid)
}

// parameterError is the front end's reply to an invalid parameter.
func parameterError(message string) *Error {
	return frontError(http.StatusBadRequest, "960", message)
}

// schemaTypeError is the request validator's reply to a top-level body
// property of the wrong type.
func schemaTypeError(property, want string, got Value) *Error {
	return parameterError(fmt.Sprintf("'%s': Expected type %s but found type %s", property, want, got.SchemaType()))
}
