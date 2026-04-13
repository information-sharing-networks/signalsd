// package schemas manages the cache of compiled json schemas used when validating signals on load
//
// Initialised on load and the refreshed by polling on an interval defined by CachePollingInterval.
//
// Note the caches are refreshed by polling because there can be multipe instances of the signalsd
// backend that all need to refresh their caches independently.
package schemas
