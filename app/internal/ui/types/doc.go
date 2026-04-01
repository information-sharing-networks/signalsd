// types package defines the types used by the UI package
//
// The AccessTokenDetails type is shared with the auth package to avoid circular imports.
// Note this is a copy of the AccessTokenResponse type in the signalsd auth package and must be kept in sync.
// (this is a demo UI that is intended to show how a standalone UI can be built on top of the signalsd API
// and therefore does not import any signalsd packages)
package types
