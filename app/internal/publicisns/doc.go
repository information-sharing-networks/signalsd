// package publicisns contains the functions needed to manage the inmemory copy
// of the list of public ISNs available on the server.
// The cache lists the public ISNs and the registered signal types
// This is used to avoid hitting the database when validating searches on public isns
// (roughly equivalent to how the claims are used for private ISN searching)
//
// the cache is loaded at start up and the refreshed at the interval defined in CacheRefreshInterval
package publicisns
