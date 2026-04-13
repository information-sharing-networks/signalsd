// package config sets up the signalsd server configuration
//
// the config is primarily defined via env variables:
//   - sensible defaults are applied for all variables for dev environments
//   - mandatory variables (DATABASE_URL etc) are checked for non-dev
//
// This package also defines the valid states of domain level enums (ValidRoles etc)
package config
