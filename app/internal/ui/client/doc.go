// client package is used to call the signalsd API.
//
// The client owns the request construction, auth headers, base URL, response decoding, and error wrapping.
// All signalsd API calls should go through the client.
//
// see server/doc.go for more information on how the ui is rendered
package client
