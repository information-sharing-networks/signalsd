// package router contains the functions needed to manage the ISN router routes cache.
// The ISN router allows signals of the same type to be submitted to a single endpoint and then
// distributed to the relevant ISN according to their data content.
//
// the cache is loaded at start up and the refreshed at the interval defined in CacheRefreshInterval
package router
